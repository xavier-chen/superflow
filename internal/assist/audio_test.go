package assist

import (
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/config"
	"testing"
)

func TestAudioPresetsAreExplicitAndAvoidSystemFeedback(t *testing.T) {
	c, _ := config.New("hub", "local")
	c.Audio.PlaySystem = []string{"@self", "audio-play"}
	next, err := AudioPreset(c, "send", "linux", nil)
	if err != nil || len(next.Audio.PlaySystem) != 0 || len(next.Audio.SendSystem) == 0 || len(next.Audio.SendMicrophone) != 0 || len(c.Audio.PlaySystem) == 0 {
		t.Fatal("preset altered microphone, source config or feedback policy", err)
	}
	if _, err = AudioPreset(c, "send", "darwin", nil); err == nil {
		t.Fatal("selected a physical microphone for system capture")
	}
	devices := []audio.Device{{ID: "virtual", Name: "BlackHole 2ch", Kind: "capture"}}
	next, err = AudioPreset(c, "send", "darwin", devices)
	if err != nil || next.Audio.SendSystem[len(next.Audio.SendSystem)-1] != "virtual" {
		t.Fatal(next, err)
	}
	devices = append(devices, audio.Device{ID: "second", Name: "BlackHole 16ch", Kind: "capture"})
	if _, err = AudioPreset(c, "send", "darwin", devices); err == nil {
		t.Fatal("guessed among multiple virtual devices")
	}
}
