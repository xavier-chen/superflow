//go:build windows

package input

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"superflow.local/superflow/internal/wire"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var setHook = user32.NewProc("SetWindowsHookExW")
var unhook = user32.NewProc("UnhookWindowsHookEx")
var nextHook = user32.NewProc("CallNextHookEx")
var getMsg = user32.NewProc("GetMessageW")
var postThread = user32.NewProc("PostThreadMessageW")
var sendInput = user32.NewProc("SendInput")
var cursorPos = user32.NewProc("GetCursorPos")
var metrics = user32.NewProc("GetSystemMetrics")

const injectedMarker = 0x53464c4f57

type point struct{ X, Y int32 }
type kbData struct {
	VK, Scan, Flags, Time uint32
	Extra                 uintptr
}
type mouseData struct {
	Point             point
	Data, Flags, Time uint32
	Extra             uintptr
}
type winDriver struct {
	mu        sync.Mutex
	thread    uint32
	fn        func(Local) bool
	callbacks [2]uintptr
	stop      sync.Once
}

func New(Options) (Driver, error) { return &winDriver{}, nil }
func dimension(n uintptr) float64 { v, _, _ := metrics.Call(n); return float64(int32(v)) }
func (d *winDriver) Capture(ctx context.Context, fn func(Local) bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if p := user32.NewProc("SetThreadDpiAwarenessContext"); p.Find() == nil {
		_, _, _ = p.Call(^uintptr(3))
	}
	d.fn = fn
	x, y, w, h := dimension(76), dimension(77), dimension(78), dimension(79)
	if w <= 1 || h <= 1 {
		return errors.New("cannot read virtual desktop bounds")
	}
	callback := func(mouse bool) uintptr {
		return windows.NewCallback(func(code int32, msg uintptr, data unsafe.Pointer) uintptr {
			if code < 0 {
				v, _, _ := nextHook.Call(0, uintptr(code), msg, uintptr(data))
				return v
			}
			var e wire.Event
			var dx, dy float64
			if mouse {
				m := (*mouseData)(data)
				if m.Flags&1 != 0 {
					v, _, _ := nextHook.Call(0, uintptr(code), msg, uintptr(data))
					return v
				}
				e.X = clamp((float64(m.Point.X) - x) / (w - 1))
				e.Y = clamp((float64(m.Point.Y) - y) / (h - 1))
				switch msg {
				case 0x200:
					e.Type = "move"
					var old point
					_, _, _ = cursorPos.Call(uintptr(unsafe.Pointer(&old)))
					dx = float64(m.Point.X-old.X) / w
					dy = float64(m.Point.Y-old.Y) / h
				case 0x201, 0x202:
					e.Type = "button"
					e.Button = 1
					e.Down = msg == 0x201
				case 0x204, 0x205:
					e.Type = "button"
					e.Button = 2
					e.Down = msg == 0x204
				case 0x207, 0x208:
					e.Type = "button"
					e.Button = 3
					e.Down = msg == 0x207
				case 0x20b, 0x20c:
					e.Type = "button"
					e.Button = 3 + uint8(m.Data>>16)
					e.Down = msg == 0x20b
				case 0x20a:
					e.Type = "scroll"
					e.DY = float64(int16(m.Data>>16)) / 120
				case 0x20e:
					e.Type = "scroll"
					e.DX = float64(int16(m.Data>>16)) / 120
				}
			} else {
				k := (*kbData)(data)
				if k.Flags&0x10 != 0 {
					v, _, _ := nextHook.Call(0, uintptr(code), msg, uintptr(data))
					return v
				}
				scan := uint16(k.Scan)
				if k.Flags&1 != 0 {
					scan |= 0x100
				}
				hid, ok := winHID[scan]
				if ok {
					e.Type = "key"
					e.Key = hid
					e.Down = k.Flags&0x80 == 0
				}
			}
			if e.Type != "" && fn(Local{Event: e, DeltaX: dx, DeltaY: dy}) {
				return 1
			}
			v, _, _ := nextHook.Call(0, uintptr(code), msg, uintptr(data))
			return v
		})
	}
	d.callbacks = [2]uintptr{callback(false), callback(true)}
	// Create the message queue before advertising the thread to cancellation.
	var msg [48]byte
	peek := user32.NewProc("PeekMessageW")
	_, _, _ = peek.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0)
	d.mu.Lock()
	d.thread = windows.GetCurrentThreadId()
	d.mu.Unlock()
	kh, _, err := setHook.Call(13, d.callbacks[0], 0, 0)
	if kh == 0 {
		return fmt.Errorf("keyboard hook: %w", err)
	}
	defer unhook.Call(kh)
	mh, _, err := setHook.Call(14, d.callbacks[1], 0, 0)
	if mh == 0 {
		return fmt.Errorf("mouse hook: %w", err)
	}
	defer unhook.Call(mh)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = d.Close()
		case <-done:
		}
	}()
	for {
		r, _, e := getMsg.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) == -1 {
			return e
		}
		if r == 0 {
			return nil
		}
	}
}
func (d *winDriver) Inject(e wire.Event) error {
	var b [40]byte
	switch e.Type {
	case "key":
		scan, ok := hidWin[e.Key]
		if !ok {
			return nil
		}
		binary.LittleEndian.PutUint32(b[:4], 1)
		binary.LittleEndian.PutUint16(b[10:12], scan&0xff)
		flags := uint32(8)
		if !e.Down {
			flags |= 2
		}
		if scan&0x100 != 0 {
			flags |= 1
		}
		binary.LittleEndian.PutUint32(b[12:16], flags)
		binary.LittleEndian.PutUint64(b[24:32], injectedMarker)
	case "move":
		binary.LittleEndian.PutUint32(b[8:12], uint32(clamp(e.X)*65535))
		binary.LittleEndian.PutUint32(b[12:16], uint32(clamp(e.Y)*65535))
		binary.LittleEndian.PutUint32(b[20:24], 0xC001)
		binary.LittleEndian.PutUint64(b[32:40], injectedMarker)
	case "button":
		var flags uint32
		switch e.Button {
		case 1:
			flags = 2
		case 2:
			flags = 8
		case 3:
			flags = 0x20
		case 4, 5:
			flags = 0x80
			binary.LittleEndian.PutUint32(b[16:20], uint32(e.Button-3))
		default:
			return nil
		}
		if !e.Down {
			flags <<= 1
		}
		binary.LittleEndian.PutUint32(b[20:24], flags)
		binary.LittleEndian.PutUint64(b[32:40], injectedMarker)
	case "scroll":
		for i, v := range []float64{e.DY, e.DX} {
			if v == 0 {
				continue
			}
			var scroll [40]byte
			binary.LittleEndian.PutUint32(scroll[16:20], uint32(int32(v*120)))
			flag := uint32(0x800)
			if i == 1 {
				flag = 0x1000
			}
			binary.LittleEndian.PutUint32(scroll[20:24], flag)
			binary.LittleEndian.PutUint64(scroll[32:40], injectedMarker)
			if err := winSend(&scroll); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
	return winSend(&b)
}
func winSend(b *[40]byte) error {
	n, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&b[0])), 40)
	if n != 1 {
		return fmt.Errorf("SendInput failed (check desktop integrity / UAC): %w", err)
	}
	return nil
}
func (d *winDriver) Close() error {
	d.stop.Do(func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.thread != 0 {
			_, _, _ = postThread.Call(uintptr(d.thread), 0x12, 0, 0)
		}
	})
	return nil
}

var hidWin = func() map[uint16]uint16 {
	m := map[uint16]uint16{}
	for h, k := range hidLinux {
		if k < 89 {
			m[h] = k
		}
	}
	for h, k := range map[uint16]uint16{70: 0x137, 73: 0x152, 74: 0x147, 75: 0x149, 76: 0x153, 77: 0x14f, 78: 0x151, 79: 0x14d, 80: 0x14b, 81: 0x150, 82: 0x148, 84: 0x135, 88: 0x11c, 101: 0x15d, 227: 0x15b, 228: 0x11d, 230: 0x138, 231: 0x15c} {
		m[h] = k
	}
	return m
}()
var winHID = func() map[uint16]uint16 {
	m := map[uint16]uint16{}
	for h, k := range hidWin {
		m[k] = h
	}
	m[86] = 100
	return m
}()
