package input

import (
	"context"
	"testing"
	"time"

	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/wire"
)

type fakeDriver struct{ events []wire.Event }

func (d *fakeDriver) Capture(context.Context, func(Local) bool) error { return nil }
func (d *fakeDriver) Inject(e wire.Event) error                       { d.events = append(d.events, e); return nil }
func (d *fakeDriver) Close() error                                    { return nil }

type fakeSink struct {
	events, motions []wire.Event
	fail            bool
}

func (s *fakeSink) SendInput(e wire.Event) bool { s.events = append(s.events, e); return !s.fail }
func (s *fakeSink) SendMotion(e wire.Event)     { s.motions = append(s.motions, e) }
func TestReceiverReleasesAndOrders(t *testing.T) {
	d := &fakeDriver{}
	r := NewReceiver(d)
	apply := func(e wire.Event) {
		t.Helper()
		if err := r.Apply(e); err != nil {
			t.Fatal(err)
		}
	}
	apply(wire.Event{Type: "move", Epoch: 1, Sequence: 5, X: .9})
	if len(d.events) != 0 {
		t.Fatal("motion before focus")
	}
	apply(wire.Event{Type: "focus", Epoch: 1, X: .5, Y: .5})
	apply(wire.Event{Type: "move", Epoch: 1, Sequence: 5, X: .8, Y: .5})
	count := len(d.events)
	apply(wire.Event{Type: "move", Epoch: 1, Sequence: 4, X: .2})
	if len(d.events) != count {
		t.Fatal("stale motion applied")
	}
	apply(wire.Event{Type: "key", Epoch: 1, Sequence: 6, Key: 225, Down: true, X: .9, Y: .5})
	apply(wire.Event{Type: "button", Epoch: 1, Sequence: 7, Button: 1, Down: true, X: .9, Y: .5})
	if err := r.Reset(); err != nil {
		t.Fatal(err)
	}
	var keyUp, buttonUp bool
	for _, e := range d.events {
		if e.Type == "key" && e.Key == 225 && !e.Down {
			keyUp = true
		}
		if e.Type == "button" && !e.Down {
			buttonUp = true
		}
	}
	if !keyUp || !buttonUp {
		t.Fatal("disconnect left injected input down")
	}
}
func TestClickRepairsLostMotion(t *testing.T) {
	d := &fakeDriver{}
	r := NewReceiver(d)
	_ = r.Apply(wire.Event{Type: "focus", Epoch: 1})
	_ = r.Apply(wire.Event{Type: "button", Epoch: 1, Sequence: 20, Button: 1, Down: true, X: .75, Y: .25})
	if len(d.events) != 3 || d.events[1].Type != "move" || d.events[1].X != .75 || d.events[2].Type != "button" {
		t.Fatalf("click snapshot: %+v", d.events)
	}
	_ = r.Apply(wire.Event{Type: "move", Epoch: 1, Sequence: 19, X: .1})
	if len(d.events) != 3 {
		t.Fatal("late motion moved cursor back")
	}
}
func TestRouterEdgesHotkeyAndDisconnect(t *testing.T) {
	d := &fakeDriver{}
	s := &fakeSink{}
	r := NewRouter("mac", []config.Screen{{Name: "mac"}, {Name: "linux", X: 1}}, d, nil)
	r.Add("linux", s)
	if !r.Handle(Local{Event: wire.Event{Type: "move", X: 1, Y: .4}, DeltaX: .01}) {
		t.Fatal("edge failed to capture")
	}
	if s.events[0].Type != "focus" {
		t.Fatal("missing focus")
	}
	r.Handle(Local{Event: wire.Event{Type: "key", Key: 4, Down: true}})
	r.Remove("linux")
	if s.events[len(s.events)-1].Type != "blur" || r.active != "mac" {
		t.Fatal("disconnect didn't return local")
	}
	r.Add("linux", s)
	r.cooldown = time.Now().Add(-time.Second)
	r.Handle(Local{Event: wire.Event{Type: "key", Key: 224, Down: true}})
	r.Handle(Local{Event: wire.Event{Type: "key", Key: 226, Down: true}})
	if !r.Handle(Local{Event: wire.Event{Type: "key", Key: 69, Down: true}}) || r.active != "linux" {
		t.Fatal("shortcut failed")
	}
	if !r.Handle(Local{Event: wire.Event{Type: "key", Key: 41, Down: true}}) || r.active != "mac" {
		t.Fatal("emergency return failed")
	}
}
func TestRouterQueueFailureRestoresLocal(t *testing.T) {
	r := NewRouter("mac", []config.Screen{{Name: "mac"}, {Name: "linux", X: 1}}, &fakeDriver{}, nil)
	s := &fakeSink{}
	r.Add("linux", s)
	r.Handle(Local{Event: wire.Event{Type: "move", X: 1}, DeltaX: .1})
	s.fail = true
	r.Handle(Local{Event: wire.Event{Type: "key", Key: 4, Down: true}})
	if r.active != "mac" {
		t.Fatal("queue overflow captured local input")
	}
}
