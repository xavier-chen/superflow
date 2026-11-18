//go:build desktop && windows && !ci

package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
	"unsafe"
)

func prepareNativeProcess() {
	id, _ := windows.UTF16PtrFromString("app.superflow.desktop")
	_, _, _ = windows.NewLazySystemDLL("shell32.dll").NewProc("SetCurrentProcessExplicitAppUserModelID").Call(uintptr(unsafe.Pointer(id)))
}
func revealNative(window fyne.Window) {
	native, ok := window.(driver.NativeWindow)
	if !ok {
		return
	}
	native.RunNative(func(value any) {
		context, ok := value.(driver.WindowsWindowContext)
		if !ok || context.HWND == 0 {
			return
		}
		user := windows.NewLazySystemDLL("user32.dll")
		iconic, _, _ := user.NewProc("IsIconic").Call(context.HWND)
		if iconic != 0 {
			_, _, _ = user.NewProc("ShowWindow").Call(context.HWND, 9)
		}
		_, _, _ = user.NewProc("SetForegroundWindow").Call(context.HWND)
	})
}
