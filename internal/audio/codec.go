package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"superflow.local/superflow/internal/wire"
)

type opusAPI struct {
	encoderCreate  func(int32, int32, int32, *int32) uintptr
	encoderDestroy func(uintptr)
	encode         func(uintptr, *int16, int32, *byte, int32) int32
	decoderCreate  func(int32, int32, *int32) uintptr
	decoderDestroy func(uintptr)
	decode         func(uintptr, *byte, int32, *int16, int32, int32) int32
}

var opus opusAPI
var opusOnce sync.Once
var opusError error

func OpusAvailable() error {
	opusOnce.Do(func() {
		h, err := openOpus()
		if err != nil {
			opusError = err
			return
		}
		defer func() {
			if p := recover(); p != nil {
				opusError = fmt.Errorf("libopus ABI: %v", p)
			}
		}()
		purego.RegisterLibFunc(&opus.encoderCreate, h, "opus_encoder_create")
		purego.RegisterLibFunc(&opus.encoderDestroy, h, "opus_encoder_destroy")
		purego.RegisterLibFunc(&opus.encode, h, "opus_encode")
		purego.RegisterLibFunc(&opus.decoderCreate, h, "opus_decoder_create")
		purego.RegisterLibFunc(&opus.decoderDestroy, h, "opus_decoder_destroy")
		purego.RegisterLibFunc(&opus.decode, h, "opus_decode")
	})
	return opusError
}

type Codec struct {
	Kind     byte
	Frames   int
	enc, dec uintptr
}

func NewCodec(kind byte, encode bool) (*Codec, error) {
	c := &Codec{Kind: kind, Frames: wire.PCMFrames}
	if kind == wire.PCM {
		return c, nil
	}
	if kind != wire.Opus {
		return nil, errors.New("unknown codec")
	}
	if err := OpusAvailable(); err != nil {
		return nil, err
	}
	c.Frames = 480
	var code int32
	if encode {
		c.enc = opus.encoderCreate(48000, 2, 2049, &code)
	} else {
		c.dec = opus.decoderCreate(48000, 2, &code)
	}
	if code != 0 || encode && c.enc == 0 || !encode && c.dec == 0 {
		return nil, fmt.Errorf("libopus initialization: %d", code)
	}
	return c, nil
}
func (c *Codec) Close() {
	if c.enc != 0 {
		opus.encoderDestroy(c.enc)
		c.enc = 0
	}
	if c.dec != 0 {
		opus.decoderDestroy(c.dec)
		c.dec = 0
	}
}
func (c *Codec) Encode(pcm []byte) ([]byte, error) {
	if len(pcm) != c.Frames*4 {
		return nil, errors.New("wrong PCM frame count")
	}
	if c.Kind == wire.PCM {
		return pcm, nil
	}
	samples := make([]int16, c.Frames*2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[2*i:]))
	}
	var out [1000]byte
	n := opus.encode(c.enc, &samples[0], int32(c.Frames), &out[0], int32(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("Opus encode: %d", n)
	}
	return append([]byte(nil), out[:n]...), nil
}
func (c *Codec) Decode(b []byte) ([]byte, error) {
	if c.Kind == wire.PCM {
		if b == nil {
			return make([]byte, wire.PCMBytes), nil
		}
		if len(b) != wire.PCMBytes {
			return nil, errors.New("wrong PCM payload length")
		}
		return b, nil
	}
	var data *byte
	if len(b) > 0 {
		data = unsafe.SliceData(b)
	}
	pcm := make([]int16, c.Frames*2)
	n := opus.decode(c.dec, data, int32(len(b)), &pcm[0], int32(c.Frames), 0)
	if n != int32(c.Frames) {
		return nil, fmt.Errorf("Opus decode frame count: %d", n)
	}
	out := make([]byte, c.Frames*4)
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out, nil
}
