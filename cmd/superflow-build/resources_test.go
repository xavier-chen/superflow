package main

import (
	"bytes"
	"debug/pe"
	"os"
	"testing"
)

func TestWindowsResourcesIncludeDPIAndIcons(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		data, err := windowsResources(arch)
		if err != nil {
			t.Fatal(err)
		}
		object, err := pe.NewFile(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		object.Close()
		if !bytes.Contains(bytes.ToLower(data), []byte("permonitorv2")) || !bytes.Contains(data, []byte("asInvoker")) {
			t.Fatal("missing native Windows manifest")
		}
	}
}
func TestPackagedWindowsGUI(t *testing.T) {
	path := os.Getenv("SUPERFLOW_WINDOWS_BINARY")
	if path == "" {
		t.Skip("set SUPERFLOW_WINDOWS_BINARY to inspect the native GUI product")
	}
	if err := verifyWindowsGUI(path, "amd64"); err != nil {
		t.Fatal(err)
	}
}
