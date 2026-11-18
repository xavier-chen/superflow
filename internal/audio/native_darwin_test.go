//go:build darwin

package audio

import "testing"

func TestAudioQueueBindings(t *testing.T) {
	if err := loadAudioQueue(); err != nil {
		t.Fatal(err)
	}
}
