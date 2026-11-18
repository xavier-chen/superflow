//go:build !linux && !windows

package input

// Native macOS coordinates already use the actual display union on each event.
func DesktopSize() (int, int) { return 0, 0 }
