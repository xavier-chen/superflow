//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"net/url"
	"path/filepath"
)

func (g *gui) advancedSettings() {
	path := widget.NewLabel(g.service.Path())
	path.Wrapping = fyne.TextWrapBreak
	open := widget.NewButtonWithIcon(tr("Open configuration folder"), theme.FolderOpenIcon(), func() { _ = g.app.OpenURL(&url.URL{Scheme: "file", Path: filepath.Dir(g.service.Path())}) })
	reload := widget.NewButtonWithIcon(tr("Reload configuration"), theme.ViewRefreshIcon(), func() { g.perform("Configuration reloaded", g.service.Reload, func() { g.showPage(5) }) })
	content := container.NewVBox(explain(tr("For manual edits, pause sharing before editing the file, then reload the configuration.")), path, container.NewHBox(open, reload))
	d := dialog.NewCustom(tr("Advanced configuration"), tr("Done"), content, g.window)
	d.Resize(fyne.NewSize(560, 250))
	d.Show()
}
