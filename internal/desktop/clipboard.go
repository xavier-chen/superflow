//go:build desktop

package desktop

import (
	"context"
	"errors"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"superflow.local/superflow/internal/clipboard"
)

// The GUI shares its native clipboard backend on all platforms, so a Linux
// desktop installation does not need wl-copy, wl-paste or xclip commands.
func (g *gui) clipboardBackend() clipboard.Backend {
	return nativeClipboard{clipboard: func() fyne.Clipboard { return g.window.Clipboard() }, dispatch: fyne.Do}
}

type nativeClipboard struct {
	clipboard func() fyne.Clipboard
	dispatch  func(func())
}

func (b nativeClipboard) Read(ctx context.Context) (string, error) {
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	b.dispatch(func() {
		if ctx.Err() != nil {
			return
		}
		value := b.clipboard().Content()
		if len(value) > clipboard.MaxText || !utf8.ValidString(value) {
			done <- result{err: errors.New(tr("Clipboard text is too long or has an unsupported format"))}
			return
		}
		done <- result{value: value}
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		return r.value, r.err
	}
}
func (b nativeClipboard) Write(ctx context.Context, value string) error {
	if len(value) > clipboard.MaxText || !utf8.ValidString(value) {
		return errors.New(tr("Clipboard text is too long or has an unsupported format"))
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	done := make(chan struct{}, 1)
	b.dispatch(func() {
		if ctx.Err() != nil {
			return
		}
		b.clipboard().SetContent(value)
		done <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
