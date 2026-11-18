package wire

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

type partial struct{ bytes.Buffer }

func (p *partial) Write(b []byte) (int, error) {
	if len(b) > 3 {
		b = b[:3]
	}
	return p.Buffer.Write(b)
}
func TestFraming(t *testing.T) {
	p := &partial{}
	want := Event{Type: "key", Key: 4, Down: true, Epoch: 7, X: .3, Y: .6}
	if err := Write(p, want); err != nil {
		t.Fatal(err)
	}
	var got Event
	if err := Read(&p.Buffer, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%+v != %+v", got, want)
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], MaxMessage+1)
	if Read(bytes.NewReader(h[:]), &got) == nil {
		t.Fatal("oversized frame accepted")
	}
}
func TestDatagramsAndValidation(t *testing.T) {
	want := Event{Type: "move", Epoch: 9, Sequence: 20, X: .3, Y: .7}
	got, err := ParseMotion(Motion(want))
	if err != nil {
		t.Fatal(err)
	}
	if got.Epoch != 9 || got.Sequence != 20 || math.Abs(got.X-want.X) > 1.0/65535 {
		t.Fatalf("motion: %+v", got)
	}
	for _, e := range []Event{{Type: "key", Key: 0}, {Type: "button", Button: 6}, {Type: "move", X: math.NaN()}, {Type: "scroll", DX: math.Inf(1)}} {
		if e.Validate() == nil {
			t.Fatalf("accepted invalid %+v", e)
		}
	}
	b, err := Audio(SystemAudio, PCM, 7, make([]byte, PCMBytes))
	if err != nil {
		t.Fatal(err)
	}
	ch, codec, seq, pcm, err := ParseAudio(b)
	if err != nil || ch != SystemAudio || codec != PCM || seq != 7 || len(pcm) != PCMBytes {
		t.Fatalf("bad audio: %v", err)
	}
	if len(b) > MaxDatagram {
		t.Fatal("packet exceeds MTU budget")
	}
	if _, _, _, _, err = ParseAudio(b[:len(b)-1]); err == nil {
		t.Fatal("short PCM accepted")
	}
}
func FuzzFrames(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Fuzz(func(t *testing.T, b []byte) {
		var e Event
		_ = Read(bytes.NewReader(b), &e)
		_, _ = ParseMotion(b)
		_, _, _, _, _ = ParseAudio(b)
	})
}
