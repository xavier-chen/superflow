package audio

import (
	"encoding/binary"
	"sync"
	"time"

	"superflow.local/superflow/internal/wire"
)

// Buffer has a fixed 64-packet horizon and never replays expired audio.
type Buffer struct {
	mu                                 sync.Mutex
	codec                              *Codec
	packets                            map[uint64][]byte
	next                               uint64
	started                            bool
	closed                             bool
	ready                              time.Time
	last                               time.Time
	buffer                             time.Duration
	current                            []byte
	Received, Concealed, Late, Trimmed uint64
}

func NewBuffer(kind byte, ms int) (*Buffer, error) {
	c, err := NewCodec(kind, false)
	if err != nil {
		return nil, err
	}
	return &Buffer{codec: c, packets: map[uint64][]byte{}, buffer: time.Duration(ms) * time.Millisecond}, nil
}
func (b *Buffer) Close() { b.mu.Lock(); defer b.mu.Unlock(); b.closed = true; b.codec.Close() }
func (b *Buffer) Insert(seq uint64, data []byte, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if !b.started || now.Sub(b.last) > time.Second {
		clear(b.packets)
		b.current = nil
		b.next = seq
		b.ready = now.Add(b.buffer)
		b.started = true
	}
	if seq < b.next {
		if now.Before(b.ready) && b.next-seq < 64 {
			b.next = seq
		} else {
			b.Late++
			return
		}
	}
	if _, ok := b.packets[seq]; ok {
		return
	}
	if len(b.packets) >= 64 {
		b.Trimmed += uint64(len(b.packets))
		clear(b.packets)
		b.current = nil
		b.next = seq
		b.ready = now.Add(b.buffer)
	}
	if seq-b.next >= 64 {
		b.Trimmed += uint64(len(b.packets))
		clear(b.packets)
		b.current = nil
		b.next = seq
		b.ready = now.Add(b.buffer)
	}
	if !now.Before(b.ready) && len(b.packets) > int(b.buffer/(time.Duration(b.codec.Frames)*time.Second/48000))+8 {
		b.Trimmed += uint64(len(b.packets))
		clear(b.packets)
		b.current = nil
		b.next = seq
	}
	b.packets[seq] = append([]byte(nil), data...)
	b.Received++
	b.last = now
}

// Read always returns five milliseconds so both codecs share one mixer cadence.
func (b *Buffer) Read(now time.Time) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || !b.started || now.Before(b.ready) || now.Sub(b.last) > time.Second {
		return nil
	}
	if len(b.current) == 0 {
		payload, ok := b.packets[b.next]
		delete(b.packets, b.next)
		b.next++
		if !ok {
			b.Concealed++
		}
		pcm, err := b.codec.Decode(payload)
		if err != nil {
			b.Concealed++
			pcm = make([]byte, b.codec.Frames*4)
		}
		b.current = pcm
	}
	out := b.current[:wire.PCMBytes]
	b.current = b.current[wire.PCMBytes:]
	return out
}
func Mix(blocks [][]byte) []byte {
	return MixWeighted(blocks, nil)
}
func MixWeighted(blocks [][]byte, gains []float64) []byte {
	out := make([]byte, wire.PCMBytes)
	for i := 0; i < wire.PCMBytes; i += 2 {
		var v float64
		for j, b := range blocks {
			if len(b) == wire.PCMBytes {
				gain := 1.0
				if j < len(gains) {
					gain = gains[j]
				}
				v += float64(int16(binary.LittleEndian.Uint16(b[i:]))) * gain
			}
		}
		if v > 32767 {
			v = 32767
		}
		if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(out[i:], uint16(int16(v)))
	}
	return out
}
