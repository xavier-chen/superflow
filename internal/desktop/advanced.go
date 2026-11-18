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
	open := widget.NewButtonWithIcon("打开配置目录", theme.FolderOpenIcon(), func() { _ = g.app.OpenURL(&url.URL{Scheme: "file", Path: filepath.Dir(g.service.Path())}) })
	reload := widget.NewButtonWithIcon("重新载入配置", theme.ViewRefreshIcon(), func() { g.perform("配置已重新载入", g.service.Reload, func() { g.showPage(5) }) })
	content := container.NewVBox(explain("常规使用无需修改配置文件。手动编辑前请先暂停共享，修改后再重新载入。"), path, container.NewHBox(open, reload))
	d := dialog.NewCustom("高级配置", "完成", content, g.window)
	d.Resize(fyne.NewSize(560, 250))
	d.Show()
}
