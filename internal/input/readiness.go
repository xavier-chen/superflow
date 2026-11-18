package input

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// SuggestedDevices only considers stable physical input paths, never a blanket
// /dev/input scan. Permissions are checked without reading input or grabbing it.
func SuggestedDevices() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	var paths []string
	for _, pattern := range []string{"/dev/input/by-id/*-event-kbd", "/dev/input/by-id/*-event-mouse", "/dev/input/by-path/*-event-kbd", "/dev/input/by-path/*-event-mouse"} {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			target, err := filepath.EvalSymlinks(path)
			if err != nil || !strings.HasPrefix(target, "/dev/input/event") {
				continue
			}
			info, err := os.Stat(target)
			if err != nil || info.Mode()&os.ModeDevice == 0 {
				continue
			}
			name, _ := os.ReadFile(filepath.Join("/sys/class/input", filepath.Base(target), "device/name"))
			if strings.Contains(strings.ToLower(string(name)), "superflow") || strings.Contains(strings.ToLower(string(name)), "soundflow") {
				continue
			}
			duplicate := false
			for _, old := range paths {
				resolved, _ := filepath.EvalSymlinks(old)
				if target == resolved {
					duplicate = true
					break
				}
			}
			if !duplicate {
				paths = append(paths, path)
			}
		}
	}
	slices.Sort(paths)
	return paths
}
