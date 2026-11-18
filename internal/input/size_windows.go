//go:build windows

package input

func DesktopSize() (int, int) { return int(dimension(78)), int(dimension(79)) }
