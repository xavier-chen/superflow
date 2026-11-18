//go:build desktop && windows

package desktop

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

func ReportStartupError(err error) {
	title, _ := windows.UTF16PtrFromString(tr("SuperFlow could not start"))
	message, _ := windows.UTF16PtrFromString(err.Error())
	_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
