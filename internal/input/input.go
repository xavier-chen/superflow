// Package input implements the KVM state machine in Go. Native code only bridges OS APIs.
package input

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/wire"
)

// Local motion includes normalized coordinates and normalized relative deltas.
type Local struct {
	wire.Event
	DeltaX, DeltaY float64
}
type Options struct {
	Devices       []string
	Width, Height int
	Capture       bool
}
type Driver interface {
	Capture(context.Context, func(Local) bool) error
	Inject(wire.Event) error
	Close() error
}
type Sink interface {
	SendInput(wire.Event) bool
	SendMotion(wire.Event)
}

// Receiver owns all injected-down state, releasing it on disconnect and focus loss.
type Receiver struct {
	mu         sync.Mutex
	driver     Driver
	epoch, seq uint64
	focused    bool
	keys       map[uint16]bool
	buttons    map[uint8]bool
}

func NewReceiver(d Driver) *Receiver {
	return &Receiver{driver: d, keys: map[uint16]bool{}, buttons: map[uint8]bool{}}
}
func (r *Receiver) reset() error {
	var err error
	for k := range r.keys {
		err = errors.Join(err, r.driver.Inject(wire.Event{Type: "key", Key: k}))
	}
	clear(r.keys)
	for b := range r.buttons {
		err = errors.Join(err, r.driver.Inject(wire.Event{Type: "button", Button: b}))
	}
	clear(r.buttons)
	r.focused = false
	return err
}
func (r *Receiver) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch = 0
	r.seq = 0
	return r.reset()
}
func (r *Receiver) Apply(e wire.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Type == "focus" {
		if e.Epoch <= r.epoch {
			return nil
		}
		if err := r.reset(); err != nil {
			return err
		}
		r.epoch = e.Epoch
		r.seq = 0
		r.focused = true
		return r.driver.Inject(wire.Event{Type: "move", X: e.X, Y: e.Y})
	}
	if !r.focused || e.Epoch != r.epoch {
		return nil
	}
	if e.Type == "blur" {
		return r.reset()
	}
	if e.Type == "move" {
		if e.Sequence <= r.seq {
			return nil
		}
		r.seq = e.Sequence
		return r.driver.Inject(e)
	}
	// A click carries its latest pointer snapshot, even when its datagram was lost.
	if e.Sequence >= r.seq {
		r.seq = e.Sequence
		if err := r.driver.Inject(wire.Event{Type: "move", X: e.X, Y: e.Y}); err != nil {
			return err
		}
	}
	switch e.Type {
	case "key":
		if !e.Down && !r.keys[e.Key] {
			return nil
		}
		r.keys[e.Key] = e.Down
		if !e.Down {
			delete(r.keys, e.Key)
		}
	case "button":
		if !e.Down && !r.buttons[e.Button] {
			return nil
		}
		r.buttons[e.Button] = e.Down
		if !e.Down {
			delete(r.buttons, e.Button)
		}
	}
	return r.driver.Inject(e)
}

type Router struct {
	mu            sync.Mutex
	local, active string
	screens       []config.Screen
	peers         map[string]Sink
	driver        Driver
	keys          map[uint16]bool
	buttons       map[uint8]bool
	epoch, seq    uint64
	x, y          float64
	cooldown      time.Time
	onChange      func(string)
}

func NewRouter(local string, screens []config.Screen, d Driver, onChange func(string)) *Router {
	return &Router{local: local, active: local, screens: screens, peers: map[string]Sink{}, driver: d,
		keys: map[uint16]bool{}, buttons: map[uint8]bool{}, x: .5, y: .5, onChange: onChange}
}
func (r *Router) Add(name string, s Sink) { r.mu.Lock(); defer r.mu.Unlock(); r.peers[name] = s }
func (r *Router) SetScreens(screens []config.Screen) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.screens = append([]config.Screen(nil), screens...)
}
func (r *Router) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == name {
		r.switchTo(r.local, .5, .5)
	}
	delete(r.peers, name)
}
func (r *Router) Local() { r.mu.Lock(); defer r.mu.Unlock(); r.switchTo(r.local, .5, .5) }
func (r *Router) Select(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buttons) > 0 {
		return errors.New("请先松开鼠标按钮")
	}
	if name != r.local {
		if r.peers[name] == nil {
			return errors.New("目标设备不在线")
		}
		found := false
		for _, s := range r.screens {
			if s.Name == name {
				found = true
				break
			}
		}
		if !found {
			return errors.New("请先将目标设备加入屏幕布局")
		}
	}
	r.switchTo(name, .5, .5)
	if r.active != name {
		return errors.New("无法切换目标，请检查输入设备权限")
	}
	return nil
}
func (r *Router) switchTo(name string, x, y float64) {
	if name == r.active {
		return
	}
	if name != r.local && r.peers[name] == nil {
		return
	}
	if driver, ok := r.driver.(interface{ SetRemote(bool) error }); ok {
		if err := driver.SetRemote(name != r.local); err != nil {
			return
		}
	}
	if s := r.peers[r.active]; s != nil {
		s.SendInput(wire.Event{Type: "blur", Epoch: r.epoch})
	}
	// Release physically held modifiers locally before suppressing subsequent events.
	if r.active == r.local && r.driver != nil {
		for k := range r.keys {
			_ = r.driver.Inject(wire.Event{Type: "key", Key: k})
		}
	}
	r.epoch++
	r.seq = 0
	r.x = clamp(x)
	r.y = clamp(y)
	r.active = name
	r.cooldown = time.Now().Add(350 * time.Millisecond)
	if s := r.peers[name]; s != nil {
		if !s.SendInput(wire.Event{Type: "focus", Epoch: r.epoch, X: r.x, Y: r.y}) {
			r.active = r.local
			if driver, ok := r.driver.(interface{ SetRemote(bool) error }); ok {
				_ = driver.SetRemote(false)
			}
		}
	} else if r.driver != nil {
		_ = r.driver.Inject(wire.Event{Type: "move", X: r.x, Y: r.y})
	}
	if r.onChange != nil {
		r.onChange(r.active)
	}
}
func clamp(v float64) float64 { return math.Max(0, math.Min(1, v)) }
func (r *Router) neighbor(dx, dy int) string {
	var cur *config.Screen
	for i := range r.screens {
		if r.screens[i].Name == r.active {
			cur = &r.screens[i]
			break
		}
	}
	if cur == nil {
		return ""
	}
	for _, s := range r.screens {
		if s.X == cur.X+dx && s.Y == cur.Y+dy && (s.Name == r.local || r.peers[s.Name] != nil) {
			return s.Name
		}
	}
	return ""
}

// Handle returns true when the physical event must be suppressed on the host.
func (r *Router) Handle(l Local) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := l.Event
	wasRemote := r.active != r.local
	if e.Type == "key" {
		repeated := e.Down && r.keys[e.Key]
		if e.Down {
			r.keys[e.Key] = true
		} else {
			delete(r.keys, e.Key)
		}
		ctrl := r.keys[224] || r.keys[228]
		alt := r.keys[226] || r.keys[230]
		if e.Key == 69 || e.Key == 41 {
			if e.Down && ctrl && alt {
				if repeated {
					return true
				}
				if wasRemote || e.Key == 41 {
					r.switchTo(r.local, .5, .5)
				} else {
					var names []string
					for _, s := range r.screens {
						if r.peers[s.Name] != nil {
							names = append(names, s.Name)
						}
					}
					sort.Strings(names)
					if len(names) > 0 {
						r.switchTo(names[0], .5, .5)
					}
				}
				return true
			}
			// Consume the shortcut's release as well.
			if !e.Down && ctrl && alt {
				return true
			}
		}
	}
	if e.Type == "button" {
		if e.Down {
			r.buttons[e.Button] = true
		} else {
			delete(r.buttons, e.Button)
		}
	}
	if e.Type == "move" {
		if wasRemote {
			r.x += l.DeltaX
			r.y += l.DeltaY
		} else {
			r.x = e.X
			r.y = e.Y
		}
		if time.Now().After(r.cooldown) && len(r.buttons) == 0 && len(r.keys) == 0 {
			dx, dy := 0, 0
			x, y := r.x, r.y
			if r.x >= .999 && l.DeltaX > 0 {
				dx = 1
				x = .002
			} else if r.x <= .001 && l.DeltaX < 0 {
				dx = -1
				x = .998
			} else if r.y >= .999 && l.DeltaY > 0 {
				dy = 1
				y = .002
			} else if r.y <= .001 && l.DeltaY < 0 {
				dy = -1
				y = .998
			}
			if name := r.neighbor(dx, dy); (dx != 0 || dy != 0) && name != "" {
				r.switchTo(name, x, y)
			}
		}
		r.x = clamp(r.x)
		r.y = clamp(r.y)
	}
	if r.active == r.local {
		return wasRemote
	}
	s := r.peers[r.active]
	if s == nil {
		r.switchTo(r.local, .5, .5)
		return false
	}
	r.seq++
	e.Epoch = r.epoch
	e.Sequence = r.seq
	e.X = r.x
	e.Y = r.y
	if e.Type == "move" {
		s.SendMotion(e)
	} else if !s.SendInput(e) {
		r.switchTo(r.local, .5, .5)
		return false
	}
	return true
}
