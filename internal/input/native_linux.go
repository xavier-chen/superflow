//go:build linux

package input

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
	"golang.org/x/sys/unix"
	"superflow.local/superflow/internal/wire"
)

const (
	uiCreate   = 0x5501
	uiDestroy  = 0x5502
	uiSetup    = 0x405c5503
	uiAbsSetup = 0x401c5504
	uiSetEV    = 0x40045564
	uiSetKey   = 0x40045565
	uiSetRel   = 0x40045566
	uiSetAbs   = 0x40045567
	uiSetProp  = 0x4004556e
	evGrab     = 0x40044590
)

type linuxDriver struct {
	mu     sync.Mutex
	opts   Options
	uinput *os.File
	x11    *xgb.Conn
	root   xproto.Window
	w, h   int
	grabMu sync.Mutex
	files  []*os.File
	remote atomic.Bool
}

func ioctl(fd int, req, arg uintptr) error {
	_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, arg)
	if e != 0 {
		return e
	}
	return nil
}

func New(o Options) (Driver, error) {
	if o.Width <= 0 || o.Height <= 0 {
		o.Width, o.Height = DesktopSize()
	}
	d := &linuxDriver{opts: o, w: o.Width, h: o.Height}
	if os.Getenv("DISPLAY") != "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		c, err := xgb.NewConn()
		if err != nil {
			return nil, err
		}
		if err = xtest.Init(c); err != nil {
			c.Close()
			return nil, err
		}
		s := xproto.Setup(c).DefaultScreen(c)
		d.x11 = c
		d.root = s.Root
		d.w = int(s.WidthInPixels)
		d.h = int(s.HeightInPixels)
		return d, nil
	}
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("Wayland 键鼠注入需要 /dev/uinput 访问权限: %w", err)
	}
	d.uinput = f
	if err = d.setup(); err != nil {
		f.Close()
		return nil, err
	}
	return d, nil
}
func (d *linuxDriver) setup() error {
	fd := int(d.uinput.Fd())
	set := func(req uintptr, values ...uintptr) error {
		for _, v := range values {
			if err := ioctl(fd, req, v); err != nil {
				return err
			}
		}
		return nil
	}
	if err := set(uiSetEV, 1, 2, 3); err != nil {
		return err
	}
	for _, code := range hidLinux {
		if err := set(uiSetKey, uintptr(code)); err != nil {
			return err
		}
	}
	if err := set(uiSetKey, 272, 273, 274, 275, 276); err != nil {
		return err
	}
	if err := set(uiSetRel, 0, 1, 6, 8); err != nil {
		return err
	}
	if err := set(uiSetAbs, 0, 1); err != nil {
		return err
	}
	if err := set(uiSetProp, 0); err != nil {
		return err
	}
	for code := uint16(0); code < 2; code++ {
		a := struct {
			Code uint16
			Pad  uint16
			Info [6]int32
		}{Code: code, Info: [6]int32{0, 0, 65535, 0, 0, 0}}
		err := ioctl(fd, uiAbsSetup, uintptr(unsafe.Pointer(&a)))
		runtime.KeepAlive(a)
		if err != nil {
			return err
		}
	}
	s := struct {
		ID   [4]uint16
		Name [80]byte
		FF   uint32
	}{ID: [4]uint16{3, 0x5346, 2, 1}}
	copy(s.Name[:], "SuperFlow v2 Keyboard Mouse")
	err := ioctl(fd, uiSetup, uintptr(unsafe.Pointer(&s)))
	runtime.KeepAlive(s)
	if err != nil {
		return err
	}
	return ioctl(fd, uiCreate, 0)
}

// Capture reads only an explicitly configured allowlist, never all input devices.
// evdev is independent of the desktop server and works with both X11 and Wayland.
func (d *linuxDriver) Capture(ctx context.Context, fn func(Local) bool) error {
	if len(d.opts.Devices) == 0 {
		return errors.New("Linux 主控需要 input_devices 指定物理键盘/鼠标的 /dev/input/by-id/...-event-* 路径与读取权限")
	}
	if d.w < 1 || d.h < 1 {
		return errors.New("Linux Wayland 主控需要配置 screen_width 和 screen_height；本阶段使用快捷键切换")
	}
	var files []*os.File
	defer func() {
		d.grabMu.Lock()
		defer d.grabMu.Unlock()
		d.files = nil
		d.remote.Store(false)
		for _, f := range files {
			_ = ioctl(int(f.Fd()), evGrab, 0)
			f.Close()
		}
	}()
	var polls []unix.PollFd
	for _, path := range d.opts.Devices {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(real, "/dev/input/event") {
			return errors.New("input_devices must resolve to /dev/input/event devices")
		}
		f, err := os.OpenFile(real, os.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		files = append(files, f)
		name := make([]byte, 256)
		err = ioctl(int(f.Fd()), 0x81004506, uintptr(unsafe.Pointer(&name[0])))
		runtime.KeepAlive(name)
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(name), "SuperFlow") || strings.HasPrefix(string(name), "SoundFlow") {
			return errors.New("refusing to capture a SuperFlow virtual device")
		}
		polls = append(polls, unix.PollFd{Fd: int32(f.Fd()), Events: unix.POLLIN})
	}
	d.grabMu.Lock()
	d.files = files
	d.grabMu.Unlock()
	x, y := .5, .5
	var event [24]byte
	for {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := unix.Poll(polls, 20); err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		for i, p := range polls {
			if p.Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
				return errors.New("physical input device disconnected")
			}
			if p.Revents&unix.POLLIN == 0 {
				continue
			}
			for {
				n, err := unix.Read(int(files[i].Fd()), event[:])
				if err == unix.EAGAIN {
					break
				}
				if err != nil {
					return err
				}
				if n != len(event) {
					return errors.New("short evdev event")
				}
				typ := binary.LittleEndian.Uint16(event[16:18])
				code := binary.LittleEndian.Uint16(event[18:20])
				value := int32(binary.LittleEndian.Uint32(event[20:24]))
				var l Local
				switch typ {
				case 1:
					if code >= 272 && code <= 276 {
						l.Type = "button"
						l.Button = uint8(code - 271)
						l.Down = value != 0
					} else if h, ok := linuxHID[code]; ok {
						l.Type = "key"
						l.Key = h
						l.Down = value != 0
					}
				case 2:
					switch code {
					case 0:
						l.Type = "move"
						l.DeltaX = float64(value) / float64(d.w)
						x = clamp(x + l.DeltaX)
					case 1:
						l.Type = "move"
						l.DeltaY = float64(value) / float64(d.h)
						y = clamp(y + l.DeltaY)
					case 8:
						l.Type = "scroll"
						l.DY = float64(value)
					case 6:
						l.Type = "scroll"
						l.DX = float64(value)
					}
				}
				if l.Type == "" {
					continue
				}
				grabbed := d.remote.Load()
				if d.x11 != nil && !grabbed {
					p, err := xproto.QueryPointer(d.x11, d.root).Reply()
					if err == nil {
						x = clamp(float64(p.RootX) / float64(d.w))
						y = clamp(float64(p.RootY) / float64(d.h))
					}
				}
				// Wayland exposes no global cursor position: hotkey switching only.
				l.X = x
				l.Y = y
				if d.x11 == nil && !grabbed {
					l.X = .5
					l.Y = .5
				}
				remote := fn(l)
				if remote != grabbed {
					x = .5
					y = .5
				}
			}
		}
	}
}

// Returning to local control releases kernel grabs immediately, even without a new event.
func (d *linuxDriver) SetRemote(remote bool) error {
	d.grabMu.Lock()
	defer d.grabMu.Unlock()
	value := uintptr(0)
	if remote {
		value = 1
	}
	for _, f := range d.files {
		if err := ioctl(int(f.Fd()), evGrab, value); err != nil {
			for _, old := range d.files {
				_ = ioctl(int(old.Fd()), evGrab, 0)
			}
			d.remote.Store(false)
			return err
		}
	}
	d.remote.Store(remote)
	return nil
}

func (d *linuxDriver) Inject(e wire.Event) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.x11 != nil {
		return d.injectX11(e)
	}
	emit := func(t, c uint16, v int32) error {
		var b [24]byte
		binary.LittleEndian.PutUint16(b[16:18], t)
		binary.LittleEndian.PutUint16(b[18:20], c)
		binary.LittleEndian.PutUint32(b[20:24], uint32(v))
		_, err := d.uinput.Write(b[:])
		return err
	}
	var err error
	v := int32(0)
	if e.Down {
		v = 1
	}
	switch e.Type {
	case "move":
		err = errors.Join(emit(3, 0, int32(clamp(e.X)*65535)), emit(3, 1, int32(clamp(e.Y)*65535)))
	case "key":
		if k, ok := hidLinux[e.Key]; ok {
			err = emit(1, k, v)
		}
	case "button":
		err = emit(1, uint16(e.Button)+271, v)
	case "scroll":
		err = errors.Join(emit(2, 6, int32(math.Round(e.DX))), emit(2, 8, int32(math.Round(e.DY))))
	}
	return errors.Join(err, emit(0, 0, 0))
}
func (d *linuxDriver) injectX11(e wire.Event) error {
	fake := func(t, detail byte, x, y int16) error {
		return xtest.FakeInputChecked(d.x11, t, detail, 0, d.root, x, y, 0).Check()
	}
	switch e.Type {
	case "move":
		return fake(6, 0, int16(clamp(e.X)*float64(d.w-1)), int16(clamp(e.Y)*float64(d.h-1)))
	case "key":
		if k, ok := hidLinux[e.Key]; ok {
			t := byte(3)
			if e.Down {
				t = 2
			}
			return fake(t, byte(k+8), 0, 0)
		}
	case "button":
		b := e.Button
		switch b {
		case 2:
			b = 3
		case 3:
			b = 2
		case 4:
			b = 8
		case 5:
			b = 9
		}
		t := byte(5)
		if e.Down {
			t = 4
		}
		return fake(t, b, 0, 0)
	case "scroll":
		for i, v := range []float64{e.DY, e.DX} {
			b := byte(4)
			if i == 1 {
				b = 7
			}
			if v < 0 {
				if i == 0 {
					b = 5
				} else {
					b = 6
				}
			}
			for n := 0; n < int(math.Abs(math.Round(v))); n++ {
				if err := fake(4, b, 0, 0); err != nil {
					return err
				}
				if err := fake(5, b, 0, 0); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (d *linuxDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.x11 != nil {
		d.x11.Close()
		d.x11 = nil
	}
	if d.uinput != nil {
		_ = ioctl(int(d.uinput.Fd()), uiDestroy, 0)
		return d.uinput.Close()
	}
	return nil
}
