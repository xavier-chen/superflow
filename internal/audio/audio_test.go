package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
	"testing"
	"time"

	"superflow.local/superflow/internal/wire"
)

func pcm(v int16) []byte {
	b := make([]byte, wire.PCMBytes)
	for i := 0; i < len(b); i += 2 {
		binary.LittleEndian.PutUint16(b[i:], uint16(v))
	}
	return b
}
func TestJitterReorderLossAndBacklog(t *testing.T) {
	b, err := NewBuffer(wire.PCM, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	now := time.Unix(100, 0)
	b.Insert(11, pcm(11), now)
	b.Insert(10, pcm(10), now.Add(time.Millisecond))
	b.Insert(13, pcm(13), now.Add(2*time.Millisecond))
	if b.Read(now) != nil {
		t.Fatal("ignored prebuffer")
	}
	for i, want := range []int16{10, 11, 0, 13} {
		out := b.Read(now.Add(time.Duration(20+5*i) * time.Millisecond))
		if len(out) != wire.PCMBytes || int16(binary.LittleEndian.Uint16(out)) != want {
			t.Fatalf("frame %d: %v", i, out[:min(2, len(out))])
		}
	}
	if b.Concealed != 1 {
		t.Fatalf("concealed %d", b.Concealed)
	}
	b.Insert(12, pcm(12), now.Add(50*time.Millisecond))
	if b.Late != 1 {
		t.Fatal("late packet accepted")
	}
	b.Insert(1000, pcm(23), now.Add(60*time.Millisecond))
	if len(b.packets) > 64 {
		t.Fatal("unbounded buffer")
	}
	if got := b.Read(now.Add(81 * time.Millisecond)); int16(binary.LittleEndian.Uint16(got)) != 23 {
		t.Fatal("did not skip stale backlog")
	}
}
func TestMixClips(t *testing.T) {
	for _, v := range []int16{30000, -30000} {
		got := Mix([][]byte{pcm(v), pcm(v)})
		want := int16(32767)
		if v < 0 {
			want = -32768
		}
		if int16(binary.LittleEndian.Uint16(got)) != want {
			t.Fatal("clipping")
		}
	}
}
func TestRingConcurrent(t *testing.T) {
	r := NewRing(128)
	want := bytes.Repeat([]byte{1, 2, 3, 4}, 10000)
	got := make([]byte, len(want))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for pos := 0; pos < len(want); {
			pos += r.Write(want[pos:])
		}
	}()
	go func() {
		defer wg.Done()
		for pos := 0; pos < len(got); {
			pos += r.Read(got[pos:])
		}
	}()
	wg.Wait()
	if !bytes.Equal(want, got) {
		t.Fatal("SPSC data corrupted")
	}
}
func TestOpusSignalAndBandwidth(t *testing.T) {
	if err := OpusAvailable(); err != nil {
		t.Skip(err)
	}
	e, err := NewCodec(wire.Opus, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	d, err := NewCodec(wire.Opus, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	total := 0
	power := 0.0
	for packet := 0; packet < 100; packet++ {
		b := make([]byte, 480*4)
		for f := 0; f < 480; f++ {
			v := int16(10000 * math.Sin(2*math.Pi*440*float64(packet*480+f)/48000))
			binary.LittleEndian.PutUint16(b[f*4:], uint16(v))
			binary.LittleEndian.PutUint16(b[f*4+2:], uint16(v))
		}
		out, err := e.Encode(b)
		if err != nil {
			t.Fatal(err)
		}
		total += len(out)
		decoded, err := d.Decode(out)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(decoded); i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(decoded[i:])))
			power += v * v
		}
	}
	rms := math.Sqrt(power / (48000 * 2))
	if rms < 1000 || rms > 15000 {
		t.Fatalf("bad decoded signal RMS %.1f", rms)
	}
	if total >= 48000*4/3 {
		t.Fatalf("compression ineffective: %d bytes", total)
	}
	t.Logf("1s sine: Opus payload=%d bytes (%.1f kbit/s), PCM=192000 bytes, RMS=%.1f", total, float64(total)*8/1000, rms)
	if _, err = d.Decode(nil); err != nil {
		t.Fatal("PLC", err)
	}
}

func TestResampleMono24kToStereo48k(t *testing.T) {
	c := Converter{Rate: 24000, Channels: 1, Bits: 16}
	total := 0
	for packet := 0; packet < 100; packet++ {
		in := make([]byte, 240*2)
		for i := 0; i < 240; i++ {
			binary.LittleEndian.PutUint16(in[2*i:], uint16(int16(5000)))
		}
		out, err := c.Convert(in, 240, false)
		if err != nil {
			t.Fatal(err)
		}
		total += len(out) / 4
		for i := 0; i < len(out); i += 4 {
			if binary.LittleEndian.Uint16(out[i:]) != binary.LittleEndian.Uint16(out[i+2:]) {
				t.Fatal("mono upmix")
			}
		}
	}
	if total < 47998 || total > 48000 {
		t.Fatalf("sample count drift: %d", total)
	}
}
