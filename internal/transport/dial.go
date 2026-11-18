package transport

import (
	"context"
	"errors"
	"github.com/quic-go/quic-go"
	"time"

	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/i18n"
)

// DialAny races invitation addresses with a small stagger. Each candidate must
// pass the existing pinned TLS handshake; every losing connection is closed.
func DialAny(parent context.Context, c *config.Config) (*Session, error) {
	addresses := []string{c.Server}
	seen := map[string]bool{c.Server: true}
	for _, address := range c.ServerAlternates {
		if !seen[address] {
			addresses = append(addresses, address)
			seen[address] = true
		}
	}
	if len(addresses) == 1 {
		return Dial(parent, c)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	type result struct {
		conn *quic.Conn
		err  error
	}
	results := make(chan result, len(addresses))
	for i, address := range addresses {
		go func() {
			if i > 0 {
				timer := time.NewTimer(time.Duration(i) * 150 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					results <- result{err: ctx.Err()}
					return
				case <-timer.C:
				}
			}
			candidate := c.Clone()
			candidate.Server = address
			conn, err := dialTLS(ctx, candidate)
			results <- result{conn, err}
		}()
	}
	remaining := len(addresses)
	defer func() {
		cancel()
		go func() {
			for ; remaining > 0; remaining-- {
				result := <-results
				if result.conn != nil {
					_ = result.conn.CloseWithError(0, "alternate address not selected")
				}
			}
		}()
	}()
	var last error
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-results:
			remaining--
			if result.err == nil {
				if ctx.Err() != nil {
					_ = result.conn.CloseWithError(0, "connection attempt canceled")
					return nil, ctx.Err()
				}
				cancel()
				return authenticateClient(parent, result.conn, c)
			}
			last = result.err
		}
	}
	if last == nil {
		last = errors.New(i18n.T("No connection addresses are available"))
	}
	return nil, last
}
