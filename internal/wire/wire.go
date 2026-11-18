// Package wire contains bounded v2 frames. QUIC supplies encryption and replay protection.
package wire

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
)

const Version = 2
const MaxMessage = 1 << 20
const MaxDatagram = 1100
const (
	StreamInput     byte = 1
	StreamClipboard byte = 2
)
const (
	DatagramMotion byte = 1
	DatagramAudio  byte = 2
)
const (
	SystemAudio byte = 1
	Microphone  byte = 2
)
const (
	PCM  byte = 1
	Opus byte = 2
)

// Keys use USB HID Keyboard/Keypad usage IDs, never OS-specific keycodes.
type Event struct {
	Type     string  `json:"type"`
	Epoch    uint64  `json:"epoch,omitempty"`
	Sequence uint64  `json:"sequence,omitempty"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Key      uint16  `json:"key,omitempty"`
	Button   uint8   `json:"button,omitempty"`
	Down     bool    `json:"down,omitempty"`
	DX       float64 `json:"dx,omitempty"`
	DY       float64 `json:"dy,omitempty"`
}

func (e Event) Validate() error {
	if math.IsNaN(e.X) || math.IsNaN(e.Y) || math.IsInf(e.X, 0) || math.IsInf(e.Y, 0) || e.X < 0 || e.X > 1 || e.Y < 0 || e.Y > 1 {
		return errors.New("invalid pointer coordinates")
	}
	switch e.Type {
	case "focus", "blur", "move":
	case "key":
		if e.Key < 4 || e.Key > 231 {
			return errors.New("invalid HID key")
		}
	case "button":
		if e.Button < 1 || e.Button > 5 {
			return errors.New("invalid button")
		}
	case "scroll":
		if math.IsNaN(e.DX) || math.IsNaN(e.DY) || math.IsInf(e.DX, 0) || math.IsInf(e.DY, 0) || math.Abs(e.DX) > 120 || math.Abs(e.DY) > 120 {
			return errors.New("invalid scroll")
		}
	default:
		return errors.New("unknown input event")
	}
	return nil
}

func Write(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxMessage {
		return errors.New("message too large")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(b)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, b)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

func Read(r io.Reader, v any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > MaxMessage {
		return errors.New("invalid frame length")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func Motion(e Event) []byte {
	b := make([]byte, 21)
	b[0] = DatagramMotion
	binary.BigEndian.PutUint64(b[1:9], e.Epoch)
	binary.BigEndian.PutUint64(b[9:17], e.Sequence)
	binary.BigEndian.PutUint16(b[17:19], uint16(math.Round(e.X*65535)))
	binary.BigEndian.PutUint16(b[19:21], uint16(math.Round(e.Y*65535)))
	return b
}

func ParseMotion(b []byte) (Event, error) {
	if len(b) != 21 || b[0] != DatagramMotion {
		return Event{}, errors.New("invalid motion")
	}
	return Event{Type: "move", Epoch: binary.BigEndian.Uint64(b[1:9]), Sequence: binary.BigEndian.Uint64(b[9:17]), X: float64(binary.BigEndian.Uint16(b[17:19])) / 65535, Y: float64(binary.BigEndian.Uint16(b[19:21])) / 65535}, nil
}

// PCM packets contain exactly five milliseconds, keeping every datagram below MTU.
const PCMFrames = 240
const PCMBytes = PCMFrames * 4
const AudioHeader = 11

func Audio(channel, codec byte, sequence uint64, payload []byte) ([]byte, error) {
	if channel != SystemAudio && channel != Microphone {
		return nil, errors.New("invalid audio channel")
	}
	if codec == PCM && len(payload) != PCMBytes || codec == Opus && (len(payload) == 0 || len(payload) > 1000) || codec != PCM && codec != Opus {
		return nil, errors.New("invalid audio payload")
	}
	b := make([]byte, AudioHeader+len(payload))
	b[0] = DatagramAudio
	b[1] = channel
	b[2] = codec
	binary.BigEndian.PutUint64(b[3:11], sequence)
	copy(b[11:], payload)
	return b, nil
}

func ParseAudio(b []byte) (channel, codec byte, sequence uint64, payload []byte, err error) {
	if len(b) < AudioHeader || b[0] != DatagramAudio {
		return 0, 0, 0, nil, errors.New("invalid audio datagram")
	}
	if b[1] != SystemAudio && b[1] != Microphone || b[2] != PCM && b[2] != Opus || b[2] == PCM && len(b) != AudioHeader+PCMBytes || b[2] == Opus && (len(b) <= AudioHeader || len(b) > AudioHeader+1000) {
		return 0, 0, 0, nil, errors.New("invalid audio payload")
	}
	return b[1], b[2], binary.BigEndian.Uint64(b[3:11]), b[11:], nil
}
