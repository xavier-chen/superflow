//go:build windows

package audio

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func openOpus() (uintptr, error) {
	path := os.Getenv("SUPERFLOW_OPUS_LIBRARY")
	if path == "" {
		path = os.Getenv("SOUNDFLOW_OPUS_LIBRARY")
	}
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return 0, err
		}
		path = filepath.Join(filepath.Dir(exe), "opus.dll")
	}
	if !filepath.IsAbs(path) {
		return 0, errors.New("SUPERFLOW_OPUS_LIBRARY must be an absolute DLL path")
	}
	dll, err := windows.LoadDLL(path)
	if err != nil {
		return 0, errors.New("未找到 opus.dll，请放在程序旁或将 audio.codec 设为 pcm")
	}
	return uintptr(dll.Handle), nil
}
