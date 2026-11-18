//go:build darwin

package input

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
	"superflow.local/superflow/internal/i18n"
	"superflow.local/superflow/internal/wire"
)

type cgPoint struct{ X, Y float64 }
type cgSize struct{ Width, Height float64 }
type cgRect struct {
	Origin cgPoint
	Size   cgSize
}
type macAPI struct {
	trusted, listenAccess func() bool
	tapCreate             func(uint32, uint32, uint32, uint64, uintptr, uintptr) uintptr
	tapEnable             func(uintptr, bool)
	sourceCreate          func(uintptr, uintptr, int64) uintptr
	runCurrent            func() uintptr
	runAdd, runRemove     func(uintptr, uintptr, uintptr)
	runMode               func(uintptr, float64, bool) int32
	release               func(uintptr)
	getLocation           func(uintptr) cgPoint
	getInt                func(uintptr, uint32) int64
	getDouble             func(uintptr, uint32) float64
	setInt                func(uintptr, uint32, int64)
	setFlags              func(uintptr, uint64)
	keyState              func(int32, uint16) bool
	mainDisplay           func() uint32
	displayBounds         func(uint32) cgRect
	activeDisplays        func(uint32, *uint32, *uint32) int32
	mouseCreate           func(uintptr, uint32, cgPoint, uint32) uintptr
	keyCreate             func(uintptr, uint16, bool) uintptr
	scrollCreate          func(uintptr, uint32, uint32, int32, int32, int32) uintptr
	eventCreate           func(uintptr) uintptr
	post                  func(uint32, uintptr)
	defaultMode           uintptr
	stringCreate          func(uintptr, string, uint32) uintptr
}

var mac macAPI
var macOnce sync.Once
var macErr error

func loadMac() error {
	macOnce.Do(func() {
		defer func() {
			if p := recover(); p != nil {
				macErr = fmt.Errorf("macOS API: %v", p)
			}
		}()
		cg, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			macErr = err
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			macErr = err
			return
		}
		for _, v := range []struct {
			fn   any
			name string
		}{{&mac.trusted, "AXIsProcessTrusted"}, {&mac.listenAccess, "CGPreflightListenEventAccess"}, {&mac.tapCreate, "CGEventTapCreate"}, {&mac.tapEnable, "CGEventTapEnable"}, {&mac.getLocation, "CGEventGetLocation"}, {&mac.getInt, "CGEventGetIntegerValueField"}, {&mac.getDouble, "CGEventGetDoubleValueField"}, {&mac.setInt, "CGEventSetIntegerValueField"}, {&mac.setFlags, "CGEventSetFlags"}, {&mac.keyState, "CGEventSourceKeyState"}, {&mac.mainDisplay, "CGMainDisplayID"}, {&mac.displayBounds, "CGDisplayBounds"}, {&mac.activeDisplays, "CGGetActiveDisplayList"}, {&mac.mouseCreate, "CGEventCreateMouseEvent"}, {&mac.keyCreate, "CGEventCreateKeyboardEvent"}, {&mac.scrollCreate, "CGEventCreateScrollWheelEvent2"}, {&mac.eventCreate, "CGEventCreate"}, {&mac.post, "CGEventPost"}} {
			purego.RegisterLibFunc(v.fn, cg, v.name)
		}
		for _, v := range []struct {
			fn   any
			name string
		}{{&mac.sourceCreate, "CFMachPortCreateRunLoopSource"}, {&mac.runCurrent, "CFRunLoopGetCurrent"}, {&mac.runAdd, "CFRunLoopAddSource"}, {&mac.runRemove, "CFRunLoopRemoveSource"}, {&mac.runMode, "CFRunLoopRunInMode"}, {&mac.release, "CFRelease"}, {&mac.stringCreate, "CFStringCreateWithCString"}} {
			purego.RegisterLibFunc(v.fn, cf, v.name)
		}
		mac.defaultMode = mac.stringCreate(0, "kCFRunLoopDefaultMode", 0x08000100)
		if mac.defaultMode == 0 {
			macErr = errors.New("cannot create CFRunLoop default mode")
		}
	})
	return macErr
}

const cgMarker int64 = 0x53464c4f57

type nativeDriver struct {
	stopped atomic.Bool
	inject  sync.Mutex
	keys    [256]bool
	buttons [5]bool
}

func New(o Options) (Driver, error) {
	if err := loadMac(); err != nil {
		return nil, err
	}
	if !mac.trusted() || o.Capture && !mac.listenAccess() {
		return nil, errors.New(i18n.T("Allow Accessibility in macOS Settings. The host also needs Input Monitoring."))
	}
	return &nativeDriver{}, nil
}
func desktop() cgRect {
	b := mac.displayBounds(mac.mainDisplay())
	var ids [32]uint32
	var n uint32
	if mac.activeDisplays(32, &ids[0], &n) == 0 {
		for _, id := range ids[:min(n, 32)] {
			r := mac.displayBounds(id)
			x, y := math.Min(b.Origin.X, r.Origin.X), math.Min(b.Origin.Y, r.Origin.Y)
			right, bottom := math.Max(b.Origin.X+b.Size.Width, r.Origin.X+r.Size.Width), math.Max(b.Origin.Y+b.Size.Height, r.Origin.Y+r.Size.Height)
			b = cgRect{cgPoint{x, y}, cgSize{right - x, bottom - y}}
		}
	}
	return b
}
func (d *nativeDriver) Capture(ctx context.Context, fn func(Local) bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	callback := purego.NewCallback(func(_ uintptr, typ uint32, event, _ uintptr) uintptr {
		if typ == 0xfffffffe || typ == 0xffffffff {
			d.stopped.Store(true)
			return event
		}
		if mac.getInt(event, 42) == cgMarker {
			return event
		}
		b := desktop()
		p := mac.getLocation(event)
		l := Local{Event: wire.Event{X: clamp((p.X - b.Origin.X) / math.Max(1, b.Size.Width-1)), Y: clamp((p.Y - b.Origin.Y) / math.Max(1, b.Size.Height-1))}}
		switch typ {
		case 5, 6, 7, 27:
			l.Type = "move"
			l.DeltaX = float64(mac.getInt(event, 4)) / math.Max(1, b.Size.Width)
			l.DeltaY = float64(mac.getInt(event, 5)) / math.Max(1, b.Size.Height)
		case 10, 11, 12:
			k := uint16(mac.getInt(event, 9))
			hid, ok := macHID[k]
			if !ok {
				return event
			}
			l.Type = "key"
			l.Key = hid
			l.Down = typ == 10 || typ == 12 && mac.keyState(0, k)
		case 1, 2, 3, 4, 25, 26:
			l.Type = "button"
			l.Button = uint8(mac.getInt(event, 3)) + 1
			l.Down = typ == 1 || typ == 3 || typ == 25
			if l.Button > 5 {
				return event
			}
		case 22:
			l.Type = "scroll"
			l.DX = math.Max(-120, math.Min(120, mac.getDouble(event, 12)))
			l.DY = math.Max(-120, math.Min(120, mac.getDouble(event, 11)))
		default:
			return event
		}
		if fn(l) {
			return 0
		}
		return event
	})
	var mask uint64
	for _, t := range []uint{1, 2, 3, 4, 5, 6, 7, 10, 11, 12, 22, 25, 26, 27} {
		mask |= 1 << t
	}
	port := mac.tapCreate(1, 0, 0, mask, callback, 0)
	if port == 0 {
		return errors.New(i18n.T("Could not create the input event tap. Check Accessibility and Input Monitoring."))
	}
	defer mac.release(port)
	source := mac.sourceCreate(0, port, 0)
	if source == 0 {
		return errors.New("CFMachPortCreateRunLoopSource failed")
	}
	defer mac.release(source)
	loop := mac.runCurrent()
	mac.runAdd(loop, source, mac.defaultMode)
	defer mac.runRemove(loop, source, mac.defaultMode)
	mac.tapEnable(port, true)
	defer mac.tapEnable(port, false)
	for ctx.Err() == nil && !d.stopped.Load() {
		mac.runMode(mac.defaultMode, .05, true)
	}
	return nil
}
func (d *nativeDriver) Inject(e wire.Event) error {
	d.inject.Lock()
	defer d.inject.Unlock()
	var event uintptr
	switch e.Type {
	case "move":
		b := desktop()
		typ, button := uint32(5), uint32(0)
		for i, v := range d.buttons {
			if v {
				button = uint32(i)
				typ = 27
				if i == 0 {
					typ = 6
				} else if i == 1 {
					typ = 7
				}
				break
			}
		}
		event = mac.mouseCreate(0, typ, cgPoint{b.Origin.X + e.X*(b.Size.Width-1), b.Origin.Y + e.Y*(b.Size.Height-1)}, button)
	case "key":
		k, ok := hidMac[e.Key]
		if !ok {
			return nil
		}
		d.keys[k] = e.Down
		event = mac.keyCreate(0, k, e.Down)
	case "button":
		code := e.Button - 1
		if code > 4 {
			return nil
		}
		d.buttons[code] = e.Down
		typ := uint32(25)
		if code == 0 {
			typ = 1
		} else if code == 1 {
			typ = 3
		}
		if !e.Down {
			typ++
		}
		current := mac.eventCreate(0)
		if current == 0 {
			return errors.New("CGEventCreate failed")
		}
		p := mac.getLocation(current)
		mac.release(current)
		event = mac.mouseCreate(0, typ, p, uint32(code))
	case "scroll":
		event = mac.scrollCreate(0, 1, 2, int32(e.DY), int32(e.DX), 0)
	default:
		return nil
	}
	if event == 0 {
		return errors.New("CGEvent creation failed")
	}
	defer mac.release(event)
	var flags uint64
	for _, v := range []struct {
		a, b uint16
		flag uint64
	}{{54, 55, 1 << 20}, {56, 60, 1 << 17}, {58, 61, 1 << 19}, {59, 62, 1 << 18}} {
		if d.keys[v.a] || d.keys[v.b] {
			flags |= v.flag
		}
	}
	mac.setFlags(event, flags)
	mac.setInt(event, 42, cgMarker)
	mac.post(0, event)
	return nil
}
func (d *nativeDriver) Close() error { d.stopped.Store(true); return nil }
