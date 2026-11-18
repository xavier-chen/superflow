//go:build desktop && !windows

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"superflow.local/superflow/internal/brand"
)

func ReportStartupError(err error) {
	a := app.NewWithID("app.superflow.desktop.error")
	a.SetIcon(fyne.NewStaticResource("SuperFlow.svg", []byte(brand.SVG)))
	a.Settings().SetTheme(superTheme{})
	window := a.NewWindow("SuperFlow 无法启动")
	message := widget.NewLabel(err.Error())
	message.Wrapping = fyne.TextWrapWord
	window.SetContent(container.NewVBox(widget.NewLabelWithStyle("暂时无法打开 SuperFlow", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), message, widget.NewButton("关闭", a.Quit)))
	window.Resize(fyne.NewSize(520, 220))
	window.ShowAndRun()
}
