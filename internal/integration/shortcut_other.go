//go:build !windows

package integration

import "errors"

func writeShortcut(string, string, string) error {
	return errors.New("Windows shortcut is unavailable on this system")
}
