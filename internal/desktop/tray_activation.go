//go:build desktop && !ci

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/systray"
)

// Tray activation follows the same path as a second launch, including native
// restoration of a minimized Windows window and refreshing the sharing state.
func installTrayActivation(reveal func()) {
	systray.SetOnTapped(func() { fyne.Do(reveal) })
}
