//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type toggle struct {
	widget.BaseWidget
	Checked   bool
	OnChanged func(bool)
	focused   bool
}

func (t *toggle) Cursor() desktop.Cursor { return desktop.PointerCursor }
func (t *toggle) FocusGained()           { t.focused = true; t.Refresh() }
func (t *toggle) FocusLost()             { t.focused = false; t.Refresh() }
func (t *toggle) TypedRune(rune)         {}
func (t *toggle) TypedKey(event *fyne.KeyEvent) {
	if event.Name == fyne.KeySpace || event.Name == fyne.KeyReturn {
		t.Tapped(nil)
	}
}

func newToggle(value bool) *toggle { t := &toggle{Checked: value}; t.ExtendBaseWidget(t); return t }
func (t *toggle) Tapped(*fyne.PointEvent) {
	t.Checked = !t.Checked
	t.Refresh()
	if t.OnChanged != nil {
		t.OnChanged(t.Checked)
	}
}
func (t *toggle) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(lineColor)
	track.CornerRadius = 12
	dot := canvas.NewCircle(panelColor)
	return &toggleRenderer{t: t, track: track, dot: dot}
}

type toggleRenderer struct {
	t     *toggle
	track *canvas.Rectangle
	dot   *canvas.Circle
}

func (r *toggleRenderer) Layout(_ fyne.Size) {
	r.track.Resize(fyne.NewSize(42, 24))
	r.dot.Resize(fyne.NewSize(18, 18))
	x := float32(3)
	if r.t.Checked {
		x = 21
	}
	r.dot.Move(fyne.NewPos(x, 3))
}
func (r *toggleRenderer) MinSize() fyne.Size { return fyne.NewSize(42, 24) }
func (r *toggleRenderer) Refresh() {
	r.track.StrokeWidth = 0
	if r.t.focused {
		r.track.StrokeColor = accentColor
		r.track.StrokeWidth = 2
	}
	r.track.FillColor = lineColor
	if r.t.Checked {
		r.track.FillColor = accentColor
	}
	r.track.Refresh()
	r.Layout(r.t.Size())
	r.dot.Refresh()
}
func (r *toggleRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.track, r.dot} }
func (r *toggleRenderer) Destroy()                     {}
