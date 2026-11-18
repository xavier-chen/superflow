//go:build desktop && !darwin

package desktop

import (
	"fyne.io/fyne/v2"
	"superflow.local/superflow/internal/brand"
)

// An opaque app mark stays legible on both light and dark Windows/Linux panels.
func systemTrayIcon() fyne.Resource {
	data, _ := brand.PNG(64)
	return fyne.NewStaticResource("superflow-tray.png", data)
}
