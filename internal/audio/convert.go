package audio

import (
	"encoding/binary"
	"errors"
	"math"
)

// Converter turns the actual WASAPI device mix format into the fixed wire format.
// State spans callbacks so non-integer sample-rate ratios don't drift per packet.
type Converter struct {
	Rate, Channels, Bits int
	Float                bool
	next                 float64
	previous             [2]float64
}

func (c *Converter) Convert(data []byte, frames int, silent bool) ([]byte, error) {
	if c.Rate < 8000 || c.Rate > 384000 || c.Channels < 1 || c.Channels > 32 || (c.Bits != 16 && c.Bits != 24 && c.Bits != 32) || (c.Float && c.Bits != 32) {
		return nil, errors.New("unsupported device mix format")
	}
	stride := c.Channels * c.Bits / 8
	if frames < 0 || frames > 1<<20 || !silent && len(data) < frames*stride {
		return nil, errors.New("invalid device audio buffer")
	}
	out := make([]byte, 0, int(float64(frames)*48000/float64(c.Rate)+2)*4)
	sample := func(frame, ch int) float64 {
		if silent {
			return 0
		}
		p := data[frame*stride+ch*c.Bits/8:]
		if c.Float {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(p)))
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return 0
			}
			return v
		}
		switch c.Bits {
		case 16:
			return float64(int16(binary.LittleEndian.Uint16(p))) / 32768
		case 24:
			v := int32(uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16)
			return float64(v<<8>>8) / 8388608
		default:
			return float64(int32(binary.LittleEndian.Uint32(p))) / 2147483648
		}
	}
	for f := 0; f < frames; f++ {
		v := [2]float64{sample(f, 0), sample(f, 0)}
		if c.Channels > 1 {
			v[1] = sample(f, 1)
		}
		if c.Channels >= 3 {
			center := sample(f, 2) * .707
			v[0] += center
			v[1] += center
		}
		for ch := 3; ch < c.Channels; ch++ {
			v[ch%2] += sample(f, ch) * .35
		}
		for c.next <= float64(f) {
			a := math.Max(0, math.Min(1, c.next-float64(f-1)))
			for ch := 0; ch < 2; ch++ {
				x := c.previous[ch] + a*(v[ch]-c.previous[ch])
				s := int16(math.Round(math.Max(-1, math.Min(32767.0/32768, x)) * 32768))
				out = append(out, byte(s), byte(uint16(s)>>8))
			}
			c.next += float64(c.Rate) / 48000
		}
		c.previous = v
	}
	c.next -= float64(frames)
	return out, nil
}
