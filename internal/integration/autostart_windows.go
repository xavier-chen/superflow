//go:build windows

package integration

import (
	"errors"
	"golang.org/x/sys/windows/registry"
)

const runKey = "Software\\Microsoft\\Windows\\CurrentVersion\\Run"

func AutoStartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue("SuperFlow")
	return err == nil && value != ""
}
func SetAutoStart(enabled bool, profiles ...string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		err = key.DeleteValue("SuperFlow")
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	executable, err := Install(profiles...)
	if err != nil {
		return err
	}
	profile := ""
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	return key.SetStringValue("SuperFlow", quoteWindows(executable)+" "+launchArguments(profile, true))
}
