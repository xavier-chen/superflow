//go:build desktop

package desktop

import (
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"superflow.local/superflow/internal/config"
)

const cellWidth float32 = 190
const cellHeight float32 = 192

type screenBoard struct {
	widget.BaseWidget
	screens  []config.Screen
	local    string
	origin   fyne.Position
	dragging bool
	changed  func()
	tiles    []*dragScreen
	readOnly bool
	focus    string
	details  map[string]string
	onTap    func(string)
}

func newScreenBoard(screens []config.Screen, local string, changed func()) *screenBoard {
	b := &screenBoard{screens: append([]config.Screen(nil), screens...), local: local, changed: changed}
	b.ExtendBaseWidget(b)
	return b
}
func (b *screenBoard) CreateRenderer() fyne.WidgetRenderer {
	r := &boardRenderer{board: b}
	b.tiles = nil
	for i, screen := range b.screens {
		detail := tr("Drag to arrange")
		if screen.Name == b.local {
			detail = tr("This computer · Drag to arrange")
		}
		base := &deviceTile{name: screen.Name, detail: detail, icon: theme.ComputerIcon(), active: screen.Name == b.local}
		if b.readOnly {
			base.detail = b.details[screen.Name]
			base.active = screen.Name == b.focus
			if screen.Name == "\x00add" {
				base.name = tr("Add device")
				base.icon = theme.ContentAddIcon()
			}
			if b.onTap != nil {
				base.tap = func() { b.onTap(screen.Name) }
			}
		}
		tile := &dragScreen{deviceTile: base, board: b, index: i}
		tile.ExtendBaseWidget(tile)
		b.tiles = append(b.tiles, tile)
		r.objects = append(r.objects, tile)
	}
	return r
}
func (b *screenBoard) bounds() (int, int, int, int) {
	if len(b.screens) == 0 {
		return 0, 0, 0, 0
	}
	minX, maxX, minY, maxY := b.screens[0].X, b.screens[0].X, b.screens[0].Y, b.screens[0].Y
	for _, s := range b.screens {
		minX = min(minX, s.X)
		maxX = max(maxX, s.X)
		minY = min(minY, s.Y)
		maxY = max(maxY, s.Y)
	}
	return minX, maxX, minY, maxY
}

type boardRenderer struct {
	board   *screenBoard
	objects []fyne.CanvasObject
}

func (r *boardRenderer) Layout(size fyne.Size) {
	minX, maxX, minY, maxY := r.board.bounds()
	origin := fyne.NewPos((size.Width-float32(maxX-minX+1)*cellWidth)/2-float32(minX)*cellWidth+10, (size.Height-float32(maxY-minY+1)*cellHeight)/2-float32(minY)*cellHeight+8)
	r.board.origin = origin
	if r.board.dragging {
		return
	}
	for i, t := range r.board.tiles {
		t.Resize(fyne.NewSize(170, 168))
		s := r.board.screens[i]
		t.Move(fyne.NewPos(origin.X+float32(s.X)*cellWidth, origin.Y+float32(s.Y)*cellHeight))
	}
}
func (r *boardRenderer) MinSize() fyne.Size {
	minX, maxX, minY, maxY := r.board.bounds()
	if r.board.readOnly {
		return fyne.NewSize(max(500, float32(maxX-minX+1)*cellWidth+24), max(190, float32(maxY-minY+1)*cellHeight+24))
	}
	return fyne.NewSize(max(660, float32(maxX-minX+1)*cellWidth+80), max(350, float32(maxY-minY+1)*cellHeight+70))
}
func (r *boardRenderer) Refresh() {
	r.Layout(r.board.Size())
	for _, t := range r.board.tiles {
		t.Refresh()
	}
}
func (r *boardRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *boardRenderer) Destroy()                     {}

type dragScreen struct {
	*deviceTile
	board *screenBoard
	index int
}

func (t *dragScreen) Dragged(event *fyne.DragEvent) {
	if t.board.readOnly {
		return
	}
	t.board.dragging = true
	t.Move(t.Position().Add(event.Dragged))
}
func (t *dragScreen) DragEnd() {
	b := t.board
	if b.readOnly {
		return
	}
	x := int(math.Round(float64((t.Position().X - b.origin.X) / cellWidth)))
	y := int(math.Round(float64((t.Position().Y - b.origin.Y) / cellHeight)))
	x = max(-16, min(16, x))
	y = max(-16, min(16, y))
	occupied := false
	for i, s := range b.screens {
		if i != t.index && s.X == x && s.Y == y {
			occupied = true
		}
	}
	if !occupied && (b.screens[t.index].X != x || b.screens[t.index].Y != y) {
		b.screens[t.index].X = x
		b.screens[t.index].Y = y
		if b.changed != nil {
			b.changed()
		}
	}
	b.dragging = false
	b.Refresh()
}

var _ fyne.Draggable = (*dragScreen)(nil)
