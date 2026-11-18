package clipboard

import (
	"context"
	"sync"
	"testing"
	"time"
)

type memory struct {
	mu     sync.Mutex
	text   string
	writes int
}

func (m *memory) Read(context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.text, nil
}
func (m *memory) Write(_ context.Context, s string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.text = s
	m.writes++
	return nil
}
func TestClipboardOptInNoInitialBroadcastAndNoEcho(t *testing.T) {
	b := &memory{text: "private existing clipboard"}
	s := NewService(b)
	changes := make(chan Update, 8)
	s.OnChange = func(u Update) { changes <- u }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	select {
	case <-changes:
		t.Fatal("existing clipboard leaked on start")
	case <-time.After(450 * time.Millisecond):
	}
	s.Push(Update{Source: "remote", Text: "from another computer"})
	select {
	case u := <-changes:
		if u.Source != "remote" {
			t.Fatal("lost origin")
		}
	case <-time.After(time.Second):
		t.Fatal("remote clipboard not applied")
	}
	select {
	case <-changes:
		t.Fatal("remote update echoed as a local edit")
	case <-time.After(450 * time.Millisecond):
	}
	b.mu.Lock()
	b.text = "local change"
	b.mu.Unlock()
	select {
	case u := <-changes:
		if u.Source != "" || u.Text != "local change" {
			t.Fatal("local edit", u)
		}
	case <-time.After(time.Second):
		t.Fatal("local edit not published")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("clipboard didn't stop")
	}
}
