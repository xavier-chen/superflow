//go:build desktop

package desktop

import (
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"superflow.local/superflow/internal/config"
)

func (g *gui) showError(err error) { dialog.ShowError(err, g.window) }
func (g *gui) inputPage() fyne.CanvasObject {
	header := g.heading("键鼠与剪贴板", "选择要在设备间共享的内容", nil)
	if g.config == nil {
		return container.NewVBox(header, space(24), card(widget.NewLabel("请先建立工作空间。")), widget.NewButton("创建工作空间", g.setup))
	}
	c := g.config.Clone()
	input, clip := newToggle(c.Input), newToggle(c.Clipboard)
	inputHint := widget.NewLabel("从屏幕边缘切换设备；断线时返回本机并释放远端按键。")
	inputHint.Wrapping = fyne.TextWrapWord
	inputHint.Importance = widget.LowImportance
	clipHint := widget.NewLabel("同步连接后新复制的文本。图片与文件暂未支持。")
	clipHint.Wrapping = fyne.TextWrapWord
	clipHint.Importance = widget.LowImportance
	inputCard := card(container.NewVBox(container.NewBorder(nil, nil, text("键鼠共享", 19, textColor, true), input, nil), space(8), inputHint))
	clipCard := card(container.NewVBox(container.NewBorder(nil, nil, text("文本剪贴板", 19, textColor, true), clip, nil), space(8), clipHint))
	shortcuts := card(container.NewVBox(text("快捷键", 17, textColor, true), space(6), widget.NewLabel("Ctrl + Alt + F12       切换首台远端设备 / 返回本机"), widget.NewLabel("Ctrl + Alt + Esc       紧急返回本机")))
	save := widget.NewButtonWithIcon("保存设置", theme.ConfirmIcon(), func() {
		next := c.Clone()
		next.Input = input.Checked
		next.Clipboard = clip.Checked
		g.applyFrom(c, next)
	})
	save.Importance = widget.HighImportance
	body := container.NewVBox(header, space(24), inputCard, space(12), clipCard, space(12), shortcuts, space(12), container.NewHBox(layout.NewSpacer(), save))
	if runtime.GOOS == "darwin" {
		body.Add(space(12))
		body.Add(container.NewHBox(widget.NewButton("辅助功能权限", func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") }), widget.NewButton("输入监控权限", func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent") })))
	}
	return container.NewVScroll(body)
}
func (g *gui) editLayout() {
	if g.config == nil {
		g.setup()
		return
	}
	c := g.config.Clone()
	var items []*widget.FormItem
	xs, ys := []*widget.Entry{}, []*widget.Entry{}
	for _, screen := range c.Screens {
		x, y := widget.NewEntry(), widget.NewEntry()
		x.SetText(strconv.Itoa(screen.X))
		y.SetText(strconv.Itoa(screen.Y))
		xs = append(xs, x)
		ys = append(ys, y)
		items = append(items, widget.NewFormItem(screen.Name, container.NewGridWithColumns(2, x, y)))
	}
	if runtime.GOOS == "linux" {
		devices := widget.NewEntry()
		devices.SetText(strings.Join(c.InputDevices, ","))
		width, height := widget.NewEntry(), widget.NewEntry()
		width.SetText(fmt.Sprint(c.ScreenWidth))
		height.SetText(fmt.Sprint(c.ScreenHeight))
		items = append(items, widget.NewFormItem("物理设备路径", devices), widget.NewFormItem("桌面宽 / 高", container.NewGridWithColumns(2, width, height)))
		d := dialog.NewForm("布局与 Linux 输入设备", "保存", "取消", items, func(ok bool) {
			if !ok {
				return
			}
			next := c.Clone()
			if err := applyCoordinates(next, xs, ys); err != nil {
				g.showError(err)
				return
			}
			next.InputDevices = nil
			for _, path := range strings.Split(devices.Text, ",") {
				if path = strings.TrimSpace(path); path != "" {
					next.InputDevices = append(next.InputDevices, path)
				}
			}
			next.ScreenWidth, _ = strconv.Atoi(width.Text)
			next.ScreenHeight, _ = strconv.Atoi(height.Text)
			g.applyFrom(c, next)
		}, g.window)
		d.Resize(fyne.NewSize(600, 450))
		d.Show()
		return
	}
	d := dialog.NewForm("屏幕位置 · 左负右正，上负下正", "保存", "取消", items, func(ok bool) {
		if !ok {
			return
		}
		next := c.Clone()
		if err := applyCoordinates(next, xs, ys); err != nil {
			g.showError(err)
			return
		}
		g.applyFrom(c, next)
	}, g.window)
	d.Resize(fyne.NewSize(520, 350))
	d.Show()
}
func applyCoordinates(c *config.Config, xs, ys []*widget.Entry) error {
	for i := range c.Screens {
		x, err := strconv.Atoi(xs[i].Text)
		if err != nil {
			return errors.New("屏幕位置须为整数")
		}
		y, err := strconv.Atoi(ys[i].Text)
		if err != nil {
			return errors.New("屏幕位置须为整数")
		}
		c.Screens[i].X = x
		c.Screens[i].Y = y
	}
	return c.Validate()
}
func (g *gui) openURL(value string) {
	u, err := url.Parse(value)
	if err == nil {
		_ = g.app.OpenURL(u)
	}
}
