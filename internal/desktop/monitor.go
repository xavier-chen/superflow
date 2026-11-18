//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The device map uses small, quiet monitor drawings rather than dashboard cards.
type monitorRenderer struct {
	frame, screen, stem, foot *canvas.Rectangle
	status                    *canvas.Circle
	name, detail              *widget.Label
	plus                      *widget.Icon
	objects                   []fyne.CanvasObject
}

func newMonitorRenderer(t *deviceTile) *monitorRenderer {
	r := &monitorRenderer{
		frame: canvas.NewRectangle(panelColor), screen: canvas.NewRectangle(background),
		stem: canvas.NewRectangle(lineColor), foot: canvas.NewRectangle(lineColor),
		status: canvas.NewCircle(lineColor),
		name:   widget.NewLabelWithStyle(t.name, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		detail: widget.NewLabelWithStyle(t.detail, fyne.TextAlignCenter, fyne.TextStyle{}),
	}
	r.frame.CornerRadius = 5
	r.frame.StrokeColor = lineColor
	r.frame.StrokeWidth = 1.2
	r.screen.CornerRadius = 3
	r.detail.Importance = widget.LowImportance
	r.name.Truncation = fyne.TextTruncateEllipsis
	r.detail.Truncation = fyne.TextTruncateEllipsis
	if t.active {
		r.frame.StrokeColor = accentColor
		r.status.FillColor = successColor
	}
	r.objects = []fyne.CanvasObject{r.stem, r.foot, r.frame, r.screen, r.name, r.status, r.detail}
	if t.icon.Name() == theme.ContentAddIcon().Name() {
		r.name.Hide()
		r.status.Hide()
		r.plus = widget.NewIcon(theme.ContentAddIcon())
		r.objects = append(r.objects, r.plus)
		r.detail.SetText("添加设备")
	}
	return r
}
func (r *monitorRenderer) Layout(size fyne.Size) {
	x := (size.Width - 154) / 2
	y := float32(8)
	r.frame.Move(fyne.NewPos(x, y))
	r.frame.Resize(fyne.NewSize(154, 98))
	r.screen.Move(fyne.NewPos(x+5, y+5))
	r.screen.Resize(fyne.NewSize(144, 82))
	r.stem.Move(fyne.NewPos(x+76, y+98))
	r.stem.Resize(fyne.NewSize(2, 10))
	r.foot.Move(fyne.NewPos(x+54, y+108))
	r.foot.Resize(fyne.NewSize(46, 2))
	r.name.Move(fyne.NewPos(x+7, y+28))
	r.name.Resize(fyne.NewSize(140, 36))
	r.status.Move(fyne.NewPos(x+140, y+91))
	r.status.Resize(fyne.NewSize(3, 3))
	r.detail.Move(fyne.NewPos(0, y+118))
	r.detail.Resize(fyne.NewSize(size.Width, 30))
	if r.plus != nil {
		r.plus.Move(fyne.NewPos(x+65, y+34))
		r.plus.Resize(fyne.NewSize(24, 24))
	}
}
func (r *monitorRenderer) MinSize() fyne.Size           { return fyne.NewSize(170, 168) }
func (r *monitorRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *monitorRenderer) Destroy()                     {}
func (r *monitorRenderer) Refresh() {
	for _, object := range r.objects {
		object.Refresh()
	}
}
