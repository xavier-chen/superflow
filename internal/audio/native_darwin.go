//go:build darwin

package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"superflow.local/superflow/internal/i18n"
)

type asbd struct {
	Rate                                                                           float64
	Format, Flags, BytesPacket, FramesPacket, BytesFrame, Channels, Bits, Reserved uint32
}
type aqBuffer struct {
	Capacity       uint32
	Data           unsafe.Pointer
	Size           uint32
	User           unsafe.Pointer
	PacketCapacity uint32
	Packets        unsafe.Pointer
	PacketCount    uint32
}
type audioAddress struct{ Selector, Scope, Element uint32 }
type queueAPI struct {
	newInput, newOutput func(*asbd, uintptr, uintptr, uintptr, uintptr, uint32, *uintptr) int32
	allocate            func(uintptr, uint32, *unsafe.Pointer) int32
	enqueue             func(uintptr, unsafe.Pointer, uint32, uintptr) int32
	start               func(uintptr, uintptr) int32
	stop, dispose       func(uintptr, bool) int32
	setProperty         func(uintptr, uint32, unsafe.Pointer, uint32) int32
	dataSize            func(uint32, *audioAddress, uint32, uintptr, *uint32) int32
	getData             func(uint32, *audioAddress, uint32, uintptr, *uint32, unsafe.Pointer) int32
	stringCreate        func(uintptr, string, uint32) uintptr
	stringGet           func(uintptr, *byte, int64, uint32) bool
	release             func(uintptr)
}

var aq queueAPI
var aqOnce sync.Once
var aqError error

func loadAudioQueue() error {
	aqOnce.Do(func() {
		defer func() {
			if p := recover(); p != nil {
				aqError = fmt.Errorf("AudioQueue API: %v", p)
			}
		}()
		libs := []struct {
			path      string
			functions []struct {
				fn   any
				name string
			}
		}{
			{"/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", []struct {
				fn   any
				name string
			}{{&aq.newInput, "AudioQueueNewInput"}, {&aq.newOutput, "AudioQueueNewOutput"}, {&aq.allocate, "AudioQueueAllocateBuffer"}, {&aq.enqueue, "AudioQueueEnqueueBuffer"}, {&aq.start, "AudioQueueStart"}, {&aq.stop, "AudioQueueStop"}, {&aq.dispose, "AudioQueueDispose"}, {&aq.setProperty, "AudioQueueSetProperty"}}},
			{"/System/Library/Frameworks/CoreAudio.framework/CoreAudio", []struct {
				fn   any
				name string
			}{{&aq.dataSize, "AudioObjectGetPropertyDataSize"}, {&aq.getData, "AudioObjectGetPropertyData"}}},
			{"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", []struct {
				fn   any
				name string
			}{{&aq.stringCreate, "CFStringCreateWithCString"}, {&aq.stringGet, "CFStringGetCString"}, {&aq.release, "CFRelease"}}},
		}
		for _, lib := range libs {
			h, err := purego.Dlopen(lib.path, purego.RTLD_NOW|purego.RTLD_LOCAL)
			if err != nil {
				aqError = err
				return
			}
			for _, fn := range lib.functions {
				purego.RegisterLibFunc(fn.fn, h, fn.name)
			}
		}
	})
	return aqError
}
func four(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}
func caGet(id uint32, selector, scope string, p unsafe.Pointer, size *uint32) bool {
	a := audioAddress{four(selector), four(scope), 0}
	return aq.getData(id, &a, 0, 0, size, p) == 0
}
func caString(id uint32, selector string) string {
	var s uintptr
	size := uint32(8)
	if !caGet(id, selector, "glob", unsafe.Pointer(&s), &size) || s == 0 {
		return ""
	}
	defer aq.release(s)
	var b [1024]byte
	if !aq.stringGet(s, &b[0], 1024, 0x08000100) {
		return ""
	}
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
func Devices() ([]Device, error) {
	if err := loadAudioQueue(); err != nil {
		return nil, err
	}
	a := audioAddress{four("dev#"), four("glob"), 0}
	var size uint32
	if aq.dataSize(1, &a, 0, 0, &size) != 0 || size > 4096 {
		return nil, errors.New("cannot enumerate CoreAudio devices")
	}
	ids := make([]uint32, size/4)
	if len(ids) == 0 {
		return nil, nil
	}
	if aq.getData(1, &a, 0, 0, &size, unsafe.Pointer(&ids[0])) != 0 {
		return nil, errors.New("CoreAudio enumeration failed")
	}
	var defaultIn, defaultOut uint32
	size = 4
	caGet(1, "dIn ", "glob", unsafe.Pointer(&defaultIn), &size)
	size = 4
	caGet(1, "dOut", "glob", unsafe.Pointer(&defaultOut), &size)
	var out []Device
	for _, id := range ids {
		uid, name := caString(id, "uid "), caString(id, "lnam")
		for _, scope := range []string{"inpt", "outp"} {
			a := audioAddress{four("stm#"), four(scope), 0}
			size = 0
			if aq.dataSize(id, &a, 0, 0, &size) != 0 || size == 0 {
				continue
			}
			kind := "capture"
			def := id == defaultIn
			if scope == "outp" {
				kind = "playback"
				def = id == defaultOut
			}
			out = append(out, Device{kind, uid, name, def})
		}
	}
	return out, nil
}
func runQueue(ctx context.Context, ring *Ring, id string, capture bool) error {
	if err := loadAudioQueue(); err != nil {
		return err
	}
	format := asbd{Rate: 48000, Format: four("lpcm"), Flags: 12, BytesPacket: 4, FramesPacket: 1, BytesFrame: 4, Channels: 2, Bits: 16}
	var queue uintptr
	var stopping atomic.Bool
	failure := make(chan error, 1)
	handle := func(q uintptr, b unsafe.Pointer) {
		if stopping.Load() {
			return
		}
		buf := (*aqBuffer)(b)
		if buf.Capacity > 1<<20 {
			return
		}
		if capture {
			if buf.Size <= buf.Capacity {
				ring.Write(unsafe.Slice((*byte)(buf.Data), buf.Size))
			}
		} else {
			data := unsafe.Slice((*byte)(buf.Data), buf.Capacity)
			n := ring.Read(data)
			clear(data[n:])
			buf.Size = buf.Capacity
		}
		if code := aq.enqueue(q, b, 0, 0); code != 0 {
			select {
			case failure <- fmt.Errorf("AudioQueue enqueue: %d", code):
			default:
			}
		}
	}
	var callback uintptr
	if capture {
		callback = purego.NewCallback(func(_ uintptr, q uintptr, b unsafe.Pointer, _ uintptr, _ uint32, _ uintptr) { handle(q, b) })
	} else {
		callback = purego.NewCallback(func(_ uintptr, q uintptr, b unsafe.Pointer) { handle(q, b) })
	}
	create := aq.newOutput
	if capture {
		create = aq.newInput
	}
	if code := create(&format, callback, 0, 0, 0, 0, &queue); code != 0 {
		return fmt.Errorf("AudioQueue create: %d", code)
	}
	defer func() { stopping.Store(true); _ = aq.stop(queue, true); _ = aq.dispose(queue, true) }()
	if id != "" {
		s := aq.stringCreate(0, id, 0x08000100)
		if s == 0 {
			return errors.New("invalid audio UID")
		}
		code := aq.setProperty(queue, four("aqcd"), unsafe.Pointer(&s), 8)
		aq.release(s)
		if code != 0 {
			return fmt.Errorf("AudioQueue device selection: %d", code)
		}
	}
	for i := 0; i < 3; i++ {
		var b unsafe.Pointer
		if code := aq.allocate(queue, 960, &b); code != 0 {
			return fmt.Errorf("AudioQueue buffer: %d", code)
		}
		buf := (*aqBuffer)(b)
		if !capture {
			clear(unsafe.Slice((*byte)(buf.Data), buf.Capacity))
			buf.Size = buf.Capacity
		}
		if code := aq.enqueue(queue, b, 0, 0); code != 0 {
			return fmt.Errorf("AudioQueue prebuffer: %d", code)
		}
	}
	if code := aq.start(queue, 0); code != 0 {
		return fmt.Errorf(i18n.T("AudioQueue start: %d (check the selected device, sample rate, and microphone permission)"), code)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-failure:
		return err
	}
}
func NativeCapture(ctx context.Context, w io.Writer, id string, loopback bool) error {
	if loopback {
		return errors.New(i18n.T("For macOS system audio capture, pass the virtual input UID with --device"))
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ring := NewRing(19200)
	done := make(chan error, 1)
	go func() { done <- runQueue(ctx, ring, id, true) }()
	clock := time.NewTicker(2 * time.Millisecond)
	defer clock.Stop()
	var b [1920]byte
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			cancel()
			<-done
			return nil
		case <-clock.C:
			for {
				n := ring.Read(b[:])
				if n == 0 {
					break
				}
				if _, err := w.Write(b[:n]); err != nil {
					cancel()
					<-done
					return err
				}
			}
		}
	}
}
func NativePlayback(ctx context.Context, r io.Reader, id string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ring := NewRing(19200)
	done := make(chan error, 1)
	go func() {
		var b [960]byte
		for {
			n, err := io.ReadFull(r, b[:])
			if n > 0 {
				ring.Write(b[:n])
			}
			if err != nil {
				done <- err
				cancel()
				return
			}
		}
	}()
	err := runQueue(ctx, ring, id, false)
	cancel()
	if rc, ok := r.(io.Closer); ok {
		_ = rc.Close()
		<-done
	}
	return err
}
