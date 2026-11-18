//go:build windows

package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsStartMenuShortcutCanBeCreatedWithoutScripts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "SuperFlow.lnk")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = writeShortcut(path, executable, launchArguments(filepath.Join(root, "profile.json"), false)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatal("missing native shell shortcut", err)
	}
	if runKey != "Software\\Microsoft\\Windows\\CurrentVersion\\Run" {
		t.Fatal("invalid per-user startup registry key")
	}
}
