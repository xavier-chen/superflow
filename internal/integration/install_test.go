package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserInstallationCreatesUsableDesktopEntry(t *testing.T) {
	root := t.TempDir()
	paths := pathsFor("linux", root, filepath.Join(root, "My Apps"), filepath.Join(root, "config"), "", "")
	source := filepath.Join(root, "portable", "superflow")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("test executable"), 0755); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profile with spaces.json")
	destination, err := installAt("linux", source, paths, profile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "test executable" {
		t.Fatal("executable was not installed", err)
	}
	entry, err := os.ReadFile(paths.Launcher)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), "--config") || !strings.Contains(string(entry), filepath.Base(profile)) || !strings.Contains(string(entry), "Terminal=false") {
		t.Fatal("desktop launch lost profile or GUI mode")
	}
	if validator, err := exec.LookPath("desktop-file-validate"); err == nil {
		if output, err := exec.Command(validator, paths.Launcher).CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
	}
	if _, err = installAt("linux", destination, paths, profile); err != nil {
		t.Fatal("install was not idempotent", err)
	}
	if _, err = os.Stat(paths.Icon); err != nil {
		t.Fatal("missing native launcher icon")
	}
}
func TestDesktopEntryEscapesFieldCodesAndRejectsNewlines(t *testing.T) {
	entry, err := desktopEntry("/opt/My App/superflow", "/opt/icon.png", true, "/tmp/profile%f.json")
	if err != nil || !strings.Contains(entry, "profile%%f.json") || !strings.Contains(entry, "--background") {
		t.Fatal(entry, err)
	}
	if _, err = desktopEntry("/tmp/app\nExec=malicious", "icon", false); err == nil {
		t.Fatal("desktop entry injected a field")
	}
	if got := launchArguments(`C:\Users\Test User\profile.json`, true); !strings.HasPrefix(got, "ui --background --config ") || strings.Contains(got, `\"`) {
		t.Fatal("invalid Windows command quoting", got)
	}
}
