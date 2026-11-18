package transport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/wire"
)

func fixture(t *testing.T) (context.Context, *config.Config, *config.Config, *quic.Listener) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	h, err := config.New("hub", "mac")
	if err != nil {
		t.Fatal(err)
	}
	n, err := config.New("node", "linux")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := h.Code()
	tls, err := ServerTLS(h)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quic.ListenAddr("127.0.0.1:0", tls, Settings())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err = n.Pair(code, listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	return ctx, h, n, listener
}
func pair(t *testing.T) (context.Context, *Session, *Session) {
	t.Helper()
	ctx, h, n, l := fixture(t)
	server := make(chan *Session, 1)
	fail := make(chan error, 1)
	go func() {
		conn, err := l.Accept(ctx)
		if err != nil {
			fail <- err
			return
		}
		s, err := Accept(ctx, conn, h)
		if err != nil {
			fail <- err
			_ = conn.CloseWithError(1, "auth failed")
			return
		}
		server <- s
	}()
	client, err := Dial(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	select {
	case s := <-server:
		t.Cleanup(s.Close)
		return ctx, s, client
	case err := <-fail:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return nil, nil, nil
}
func TestEncryptedMultiplexedSession(t *testing.T) {
	ctx, server, client := pair(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := make(chan wire.Event, 16)
	clips := make(chan string, 1)
	data := make(chan []byte, 8)
	finished := make(chan error, 2)
	go func() { finished <- server.Run(ctx, Handlers{Clipboard: func(s string) { clips <- s }}) }()
	go func() {
		finished <- client.Run(ctx, Handlers{Input: func(e wire.Event) { events <- e }, Datagram: func(b []byte) {
			select {
			case data <- b:
			default:
			}
		}})
	}()
	// Large clipboard data shares neither input framing nor input scheduling priority.
	text := strings.Repeat("网络 clipboard\n", 30000)
	client.SendClipboard(text)
	if !server.SendInput(wire.Event{Type: "focus", Epoch: 1, X: .5, Y: .5}) {
		t.Fatal("send focus")
	}
	if !server.SendInput(wire.Event{Type: "key", Epoch: 1, Sequence: 1, Key: 4, Down: true, X: .5, Y: .5}) {
		t.Fatal("send key")
	}
	server.SendMotion(wire.Event{Type: "move", Epoch: 1, Sequence: 2, X: .75, Y: .25})
	pcm, _ := wire.Audio(wire.SystemAudio, wire.PCM, 3, make([]byte, wire.PCMBytes))
	server.SendAudio(pcm)
	for _, want := range []string{"focus", "key"} {
		select {
		case e := <-events:
			if e.Type != want {
				t.Fatalf("ordering: %+v", e)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("input blocked")
		}
	}
	select {
	case got := <-clips:
		if got != text {
			t.Fatal("clipboard corrupted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("clipboard not delivered")
	}
	seen := map[byte]bool{}
	for len(seen) < 2 {
		select {
		case b := <-data:
			seen[b[0]] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("missing datagram lanes: %v", seen)
		}
	}
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Fatal("session goroutines leaked")
		}
	}
}
func TestRejectWrongToken(t *testing.T) {
	ctx, h, n, l := fixture(t)
	n.Secret = strings.Repeat("x", 43)
	accepted := make(chan error, 1)
	go func() {
		conn, err := l.Accept(ctx)
		if err != nil {
			accepted <- err
			return
		}
		_, err = Accept(ctx, conn, h)
		accepted <- err
		_ = conn.CloseWithError(1, "rejected")
	}()
	if s, err := Dial(ctx, n); err == nil {
		s.Close()
		t.Fatal("wrong token accepted")
	}
	if err := <-accepted; err == nil {
		t.Fatal("server accepted wrong token")
	}
}
func TestRejectWrongCertificate(t *testing.T) {
	ctx, _, n, _ := fixture(t)
	n.Pin = strings.Repeat("00", 32)
	if s, err := Dial(ctx, n); err == nil {
		s.Close()
		t.Fatal("certificate pin not checked")
	}
}
