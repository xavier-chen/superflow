package audio

import (
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/wire"
	"testing"
	"time"
)

func TestReceiveCodecChangeRejectsLatePreviousFormat(t *testing.T) {
	if err := OpusAvailable(); err != nil {
		t.Skip(err)
	}
	m := NewManager(config.Audio{Codec: "auto", BufferMS: 40, PlaySystem: []string{"test"}})
	encoder, err := NewCodec(wire.Opus, true)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	payload, err := encoder.Encode(make([]byte, encoder.Frames*4))
	if err != nil {
		t.Fatal(err)
	}
	key := sourceKey{"peer", wire.SystemAudio}
	feed := func(codec byte, seq uint64, data []byte) {
		b, err := wire.Audio(wire.SystemAudio, codec, seq, data)
		if err != nil {
			t.Fatal(err)
		}
		m.Receive("peer", b)
	}
	feed(wire.PCM, 10, make([]byte, wire.PCMBytes))
	feed(wire.Opus, 20, payload)
	feed(wire.PCM, 15, make([]byte, wire.PCMBytes))
	if m.buffers[key].codec.Kind != wire.Opus {
		t.Fatal("late old codec reverted the negotiated stream")
	}
	feed(wire.PCM, 21, make([]byte, wire.PCMBytes))
	if m.buffers[key].codec.Kind != wire.PCM {
		t.Fatal("PCM fallback was not accepted")
	}
	m.buffers[key].Read(time.Now().Add(time.Second))
	m.Remove("peer")
}
func TestUnsupportedCodecDoesNotBlockPCMRecovery(t *testing.T) {
	m := NewManager(config.Audio{BufferMS: 40, PlaySystem: []string{"test"}})
	m.failed[sourceKey{"peer", wire.SystemAudio}] = wire.Opus
	packet, _ := wire.Audio(wire.SystemAudio, wire.PCM, 0, make([]byte, wire.PCMBytes))
	m.Receive("peer", packet)
	if len(m.buffers) != 1 {
		t.Fatal("fallback remained blocked by an earlier codec failure")
	}
	m.Remove("peer")
}
