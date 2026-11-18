//go:build windows

package audio

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"

	"superflow.local/superflow/internal/i18n"
)

func openOpus() (uintptr, error) {
	path := os.Getenv("SUPERFLOW_OPUS_LIBRARY")
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
		return 0, errors.New(i18n.T("opus.dll was not found. Place it next to the app or set audio.codec to pcm."))
	}
	return uintptr(dll.Handle), nil
}
