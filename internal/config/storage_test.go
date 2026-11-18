package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMigrationPreservesPairingAndOriginal(t *testing.T) {
	old := filepath.Join(t.TempDir(), "SoundFlow", "v2.json")
	next := filepath.Join(t.TempDir(), "SuperFlow", "v2.json")
	want, err := New("hub", "mac")
	if err != nil {
		t.Fatal(err)
	}
	if err = Save(old, want); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := migrate(old, next)
			if err != nil {
				t.Error(err)
				return
			}
			if got.ID != want.ID || got.Secret != want.Secret || got.Pin != want.Pin || got.PrivateKey != want.PrivateKey {
				t.Error("migration replaced pairing identity")
			}
		}()
	}
	wg.Wait()
	if _, err = os.Stat(old); err != nil {
		t.Fatal("migration removed original")
	}
}
func TestReplaceDetectsConflictsAndRetainsIdentity(t *testing.T) {
	c, _ := New("hub", "mac")
	path := filepath.Join(t.TempDir(), "v2.json")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	next := c.Clone()
	next.Clipboard = true
	next.Audio.BufferMS = 60
	if err := Replace(path, c, next); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, c, c); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale editor overwrote settings: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Secret != c.Secret || got.Pin != c.Pin || got.ID != c.ID || got.PrivateKey != c.PrivateKey || !got.Clipboard {
		t.Fatal("settings update changed pairing")
	}
	next.Screens[0].Name = "changed"
	if c.Screens[0].Name != "mac" {
		t.Fatal("settings clone shares screen storage")
	}
}
