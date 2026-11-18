//go:build darwin

package input

import "testing"

func TestCoreGraphicsBindings(t *testing.T) {
	// Loads APIs and reads geometry only; never creates a hook or injects input.
	if err := loadMac(); err != nil {
		t.Fatal(err)
	}
	b := desktop()
	if b.Size.Width <= 0 || b.Size.Height <= 0 {
		t.Fatalf("invalid CoreGraphics CGRect ABI: %+v", b)
	}
}
