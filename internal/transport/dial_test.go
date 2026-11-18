package transport

import (
	"testing"
	"time"
)

func TestInvitationTriesAlternateWithoutWaitingForDeadAddress(t *testing.T) {
	ctx, hub, node, listener := fixture(t)
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			done <- err
			return
		}
		s, err := Accept(ctx, conn, hub)
		if err == nil {
			defer s.Close()
			<-ctx.Done()
		}
		done <- err
	}()
	node.ServerAlternates = []string{node.Server}
	node.Server = "127.0.0.1:1"
	started := time.Now()
	s, err := DialAny(ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if time.Since(started) > 2*time.Second {
		t.Fatal("a dead interface stalled alternate address discovery")
	}
	if s.Peer.ID != hub.ID {
		t.Fatal("connected without pinned identity")
	}
}
