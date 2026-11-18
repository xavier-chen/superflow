//go:build windows

package integration

import (
	"errors"
	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"os"
	"path/filepath"
	"runtime"
)

func writeShortcut(path, executable, arguments string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		var native *ole.OleError
		if !errors.As(err, &native) || native.Code() != 1 {
			return err
		}
	}
	defer ole.CoUninitialize()
	object, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return err
	}
	defer object.Release()
	shell, err := object.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	value, err := oleutil.CallMethod(shell, "CreateShortcut", path)
	if err != nil {
		return err
	}
	defer value.Clear()
	shortcut := value.ToIDispatch()
	for _, property := range []struct {
		name  string
		value any
	}{{"TargetPath", executable}, {"Arguments", arguments}, {"WorkingDirectory", filepath.Dir(executable)}, {"IconLocation", executable + ",0"}, {"Description", "SuperFlow"}} {
		v, e := oleutil.PutProperty(shortcut, property.name, property.value)
		if e != nil {
			return e
		}
		if v != nil {
			_ = v.Clear()
		}
	}
	saved, err := oleutil.CallMethod(shortcut, "Save")
	if saved != nil {
		_ = saved.Clear()
	}
	return err
}
