package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPairingAndSave(t *testing.T) {
	h, err := New("hub", "mac")
	if err != nil {
		t.Fatal(err)
	}
	code, err := h.Code()
	if err != nil {
		t.Fatal(err)
	}
	n, err := New("node", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if err = n.Pair(code, "127.0.0.1:42800"); err != nil {
		t.Fatal(err)
	}
	if n.Secret != h.Secret || n.Pin != h.Pin {
		t.Fatal("pairing lost identity")
	}
	if err = n.Pair("SF1-old-key", "localhost:42800"); err == nil {
		t.Fatal("v1 key accepted")
	}
	path := filepath.Join(t.TempDir(), "v2.json")
	if err = Save(path, h); err != nil {
		t.Fatal(err)
	}
	if err = Save(path, n); err == nil {
		t.Fatal("overwrote existing identity")
	}
	got, err := Load(path)
	if err != nil || got.ID != h.ID {
		t.Fatal("save/load", err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode %v", info.Mode())
		}
	}
}
func TestRejectUnsafeConfig(t *testing.T) {
	c, _ := New("hub", "mac")
	c.Audio.SendMicrophone = []string{"@self", "audio-capture"}
	if c.Validate() == nil {
		t.Fatal("broadcast microphone accepted")
	}
	c.Audio.Target = "linux"
	c.Screens = append(c.Screens, Screen{Name: "linux"})
	if c.Validate() == nil {
		t.Fatal("overlapping screens accepted")
	}
}
