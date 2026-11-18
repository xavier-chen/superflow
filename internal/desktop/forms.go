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

func (g *gui) showError(err error) {
	message := explain(err.Error())
	d := dialog.NewCustom(tr("Error"), tr("OK"), message, g.window)
	d.Resize(fyne.NewSize(460, 190))
	d.Show()
}
func (g *gui) inputPage() fyne.CanvasObject {
	header := g.heading(tr("Input & clipboard"), tr("Choose what to share between devices"), nil)
	if g.config == nil {
		return container.NewVBox(header, space(24), card(widget.NewLabel(tr("Set up a workspace first."))), widget.NewButton(tr("Create workspace"), g.setup))
	}
	c := g.config.Clone()
	input, clip := newToggle(c.Input), newToggle(c.Clipboard)
	inputHint := widget.NewLabel(tr("Move across screen edges to switch computers. On disconnect, input returns here and remote keys are released."))
	inputHint.Wrapping = fyne.TextWrapWord
	inputHint.Importance = widget.LowImportance
	clipHint := widget.NewLabel(tr("Sync text copied after connecting. Images and files are currently unsupported."))
	clipHint.Wrapping = fyne.TextWrapWord
	clipHint.Importance = widget.LowImportance
	inputCard := card(container.NewVBox(container.NewBorder(nil, nil, text(tr("Input sharing"), 19, textColor, true), input, nil), space(8), inputHint))
	clipCard := card(container.NewVBox(container.NewBorder(nil, nil, text(tr("Text clipboard"), 19, textColor, true), clip, nil), space(8), clipHint))
	shortcuts := card(container.NewVBox(text(tr("Shortcuts"), 17, textColor, true), space(6), widget.NewLabel(tr("Ctrl + Alt + F12       Switch to the first remote computer or return here")), widget.NewLabel(tr("Ctrl + Alt + Esc       Return to this computer immediately"))))
	save := widget.NewButtonWithIcon(tr("Save settings"), theme.ConfirmIcon(), func() {
		next := c.Clone()
		next.Input = input.Checked
		next.Clipboard = clip.Checked
		g.applyFrom(c, next)
	})
	save.Importance = widget.HighImportance
	body := container.NewVBox(header, space(24), inputCard, space(12), clipCard, space(12), shortcuts, space(12), container.NewHBox(layout.NewSpacer(), save))
	if runtime.GOOS == "darwin" {
		body.Add(space(12))
		body.Add(container.NewHBox(widget.NewButton(tr("Accessibility permissions"), func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") }), widget.NewButton(tr("Input Monitoring permissions"), func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent") })))
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
		items = append(items, widget.NewFormItem(tr("Physical device paths"), devices), widget.NewFormItem(tr("Desktop width / height"), container.NewGridWithColumns(2, width, height)))
		d := dialog.NewForm(tr("Layout and Linux input devices"), tr("Save"), tr("Cancel"), items, func(ok bool) {
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
	d := dialog.NewForm(tr("Screen coordinates · Right and down are positive"), tr("Save"), tr("Cancel"), items, func(ok bool) {
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
			return errors.New(tr("Screen coordinates must be integers"))
		}
		y, err := strconv.Atoi(ys[i].Text)
		if err != nil {
			return errors.New(tr("Screen coordinates must be integers"))
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
