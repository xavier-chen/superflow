//go:build desktop && darwin

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"superflow.local/superflow/internal/brand"
)

func systemTrayIcon() fyne.Resource {
	return theme.NewThemedResource(fyne.NewStaticResource("superflow-tray.svg", []byte(brand.TraySVG)))
}
