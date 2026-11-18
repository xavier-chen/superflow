//go:build windows

package clipboard

import (
	"context"
	"errors"
	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
	"runtime"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"
)

var u32 = windows.NewLazySystemDLL("user32.dll")
var kernel = windows.NewLazySystemDLL("kernel32.dll")
var openClipboard = u32.NewProc("OpenClipboard")
var closeClipboard = u32.NewProc("CloseClipboard")
var emptyClipboard = u32.NewProc("EmptyClipboard")
var getClipboard = u32.NewProc("GetClipboardData")
var setClipboard = u32.NewProc("SetClipboardData")
var globalAlloc = kernel.NewProc("GlobalAlloc")
var globalFree = kernel.NewProc("GlobalFree")
var globalLock = kernel.NewProc("GlobalLock")
var globalUnlock = kernel.NewProc("GlobalUnlock")
var globalSize = kernel.NewProc("GlobalSize")
var globalLockOnce sync.Once
var globalLockPointer func(uintptr) unsafe.Pointer

func lockMemory(handle uintptr) unsafe.Pointer {
	globalLockOnce.Do(func() { purego.RegisterFunc(&globalLockPointer, globalLock.Addr()) })
	return globalLockPointer(handle)
}

type winClipboard struct{}

func newWindows() (Backend, error) { return winClipboard{}, nil }
func lock(ctx context.Context, owner uintptr) error {
	for i := 0; i < 20; i++ {
		if n, _, _ := openClipboard.Call(owner); n != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return errors.New("clipboard is busy")
}
func (winClipboard) Read(ctx context.Context) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := lock(ctx, 0); err != nil {
		return "", err
	}
	defer closeClipboard.Call()
	h, _, _ := getClipboard.Call(13)
	if h == 0 {
		return "", errors.New("clipboard has no Unicode text")
	}
	n, _, _ := globalSize.Call(h)
	if n > MaxText*2 || n < 2 {
		return "", errors.New("invalid clipboard allocation")
	}
	p := lockMemory(h)
	if p == nil {
		return "", errors.New("GlobalLock failed")
	}
	defer globalUnlock.Call(h)
	b := unsafe.Slice((*uint16)(p), n/2)
	end := 0
	for end < len(b) && b[end] != 0 {
		end++
	}
	text := string(utf16.Decode(b[:end]))
	if len(text) > MaxText {
		return "", errors.New("clipboard exceeds limit")
	}
	return text, nil
}
func (winClipboard) Write(ctx context.Context, text string) error {
	if len(text) > MaxText {
		return errors.New("clipboard exceeds limit")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	create := u32.NewProc("CreateWindowExW")
	destroy := u32.NewProc("DestroyWindow")
	owner, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
	if owner == 0 {
		return err
	}
	defer destroy.Call(owner)
	if err := lock(ctx, owner); err != nil {
		return err
	}
	defer closeClipboard.Call()
	b := append(utf16.Encode([]rune(text)), 0)
	h, _, _ := globalAlloc.Call(2, uintptr(len(b)*2))
	if h == 0 {
		return errors.New("GlobalAlloc failed")
	}
	owned := false
	defer func() {
		if !owned {
			_, _, _ = globalFree.Call(h)
		}
	}()
	p := lockMemory(h)
	if p == nil {
		return errors.New("GlobalLock failed")
	}
	copy(unsafe.Slice((*uint16)(p), len(b)), b)
	_, _, _ = globalUnlock.Call(h)
	if n, _, _ := emptyClipboard.Call(); n == 0 {
		return errors.New("EmptyClipboard failed")
	}
	if n, _, _ := setClipboard.Call(13, h); n == 0 {
		return errors.New("SetClipboardData failed")
	}
	owned = true
	return nil
}
