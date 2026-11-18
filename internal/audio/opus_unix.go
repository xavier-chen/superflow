//go:build darwin || linux

package audio

import (
	"errors"
	"github.com/ebitengine/purego"
	"os"
	"path/filepath"
	"runtime"
)

func openOpus() (uintptr, error) {
	names := []string{"libopus.so.0", "libopus.so"}
	if runtime.GOOS == "darwin" {
		names = []string{"libopus.0.dylib", "/opt/homebrew/lib/libopus.0.dylib", "/usr/local/lib/libopus.0.dylib"}
		if exe, err := os.Executable(); err == nil {
			names = append([]string{filepath.Join(filepath.Dir(exe), "..", "Frameworks", "libopus.0.dylib")}, names...)
		}
	} else if exe, err := os.Executable(); err == nil {
		names = append([]string{filepath.Join(filepath.Dir(exe), "lib", "libopus.so.0")}, names...)
	}
	if path := os.Getenv("SUPERFLOW_OPUS_LIBRARY"); path != "" {
		names = []string{path}
	} else if path := os.Getenv("SOUNDFLOW_OPUS_LIBRARY"); path != "" {
		names = []string{path}
	}
	var last error
	for _, name := range names {
		h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err == nil {
			return h, nil
		}
		last = err
	}
	return 0, errors.Join(errors.New("未找到 libopus，请安装 Opus 或将 audio.codec 设为 pcm"), last)
}
