//go:build desktop

package desktop

import (
	"context"
	"fyne.io/fyne/v2"
	"strings"
	"superflow.local/superflow/internal/clipboard"
	"testing"
)

type memoryClipboard struct {
	value string
	reads int
}

func (m *memoryClipboard) Content() string         { m.reads++; return m.value }
func (m *memoryClipboard) SetContent(value string) { m.value = value }
func TestNativeClipboardBoundsAndCancellation(t *testing.T) {
	backend := &memoryClipboard{}
	bridge := nativeClipboard{clipboard: func() fyne.Clipboard { return backend }, dispatch: func(fn func()) { fn() }}
	if err := bridge.Write(context.Background(), "跨平台剪贴板"); err != nil {
		t.Fatal(err)
	}
	if value, err := bridge.Read(context.Background()); err != nil || value != "跨平台剪贴板" {
		t.Fatal(value, err)
	}
	backend.value = strings.Repeat("x", clipboard.MaxText+1)
	if _, err := bridge.Read(context.Background()); err == nil {
		t.Fatal("oversized native clipboard accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := backend.reads
	if _, err := bridge.Read(ctx); err == nil || backend.reads != before {
		t.Fatal("read clipboard after cancellation")
	}
}
