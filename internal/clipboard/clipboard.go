// Package clipboard handles explicit, bounded text clipboard synchronization.
package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
	"unicode/utf8"

	"superflow.local/superflow/internal/wire"
)

const MaxText = wire.MaxMessage - 128

type Backend interface {
	Read(context.Context) (string, error)
	Write(context.Context, string) error
}
type commands struct{ read, write []string }
type bounded struct{ bytes.Buffer }

func (b *bounded) Write(p []byte) (int, error) {
	if b.Len()+len(p) > MaxText {
		return 0, errors.New("clipboard exceeds limit")
	}
	return b.Buffer.Write(p)
}

func New() (Backend, error) {
	switch runtime.GOOS {
	case "darwin":
		return commands{[]string{"/usr/bin/pbpaste"}, []string{"/usr/bin/pbcopy"}}, nil
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if _, err := exec.LookPath("wl-paste"); err != nil {
				return nil, err
			}
			if _, err := exec.LookPath("wl-copy"); err != nil {
				return nil, err
			}
			return commands{[]string{"wl-paste", "--no-newline", "--type", "text"}, []string{"wl-copy", "--type", "text/plain;charset=utf-8"}}, nil
		}
		if _, err := exec.LookPath("xclip"); err != nil {
			return nil, err
		}
		return commands{[]string{"xclip", "-selection", "clipboard", "-out"}, []string{"xclip", "-selection", "clipboard", "-in"}}, nil
	case "windows":
		return newWindows()
	}
	return nil, errors.New("unsupported clipboard platform")
}
func (c commands) Read(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.read[0], c.read[1:]...)
	var out bounded
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if !utf8.Valid(out.Bytes()) {
		return "", errors.New("clipboard is not UTF-8 text")
	}
	return out.String(), nil
}
func (c commands) Write(ctx context.Context, text string) error {
	if len(text) > MaxText || !utf8.ValidString(text) {
		return errors.New("invalid clipboard text")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.write[0], c.write[1:]...)
	cmd.Stdin = bytes.NewBufferString(text)
	// xclip / wl-copy may fork a selection owner. Do not wait for inherited pipes.
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

type Update struct{ Source, Text string }
type Service struct {
	Backend  Backend
	Incoming chan Update
	OnChange func(Update)
	OnError  func(error)
}

func NewService(b Backend) *Service { return &Service{Backend: b, Incoming: make(chan Update, 16)} }
func (s *Service) Push(u Update) {
	if len(u.Text) > MaxText {
		return
	}
	select {
	case s.Incoming <- u:
	default:
		select {
		case <-s.Incoming:
		default:
		}
		select {
		case s.Incoming <- u:
		default:
		}
	}
}
func (s *Service) Run(ctx context.Context) {
	// The existing clipboard is never broadcast simply because a device connects.
	last, err := s.Backend.Read(ctx)
	initialized := err == nil
	clock := time.NewTicker(400 * time.Millisecond)
	defer clock.Stop()
	lastError := time.Time{}
	report := func(err error) {
		if err != nil && s.OnError != nil && time.Since(lastError) > 10*time.Second {
			lastError = time.Now()
			s.OnError(fmt.Errorf("clipboard: %w", err))
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-s.Incoming:
			if !utf8.ValidString(u.Text) {
				continue
			}
			if initialized && u.Text == last {
				continue
			}
			if err := s.Backend.Write(ctx, u.Text); err != nil {
				report(err)
				continue
			}
			last = u.Text
			initialized = true
			if s.OnChange != nil {
				s.OnChange(u)
			}
		case <-clock.C:
			text, err := s.Backend.Read(ctx)
			if err != nil {
				report(err)
				continue
			}
			if !initialized {
				last = text
				initialized = true
				continue
			}
			if text == last {
				continue
			}
			last = text
			if s.OnChange != nil {
				s.OnChange(Update{Text: text})
			}
		}
	}
}
