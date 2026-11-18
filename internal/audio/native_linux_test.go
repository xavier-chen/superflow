//go:build linux

package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

type meter struct {
	mu     sync.Mutex
	frames int
	power  float64
	ready  chan struct{}
	once   sync.Once
}

func (m *meter) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := 0; i+3 < len(b); i += 4 {
		v := float64(int16(binary.LittleEndian.Uint16(b[i:])))
		m.frames++
		m.power += v * v
	}
	if m.frames >= 48000 {
		m.once.Do(func() { close(m.ready) })
	}
	return len(b), nil
}
func TestNativePulseLoopback(t *testing.T) {
	if os.Getenv("SUPERFLOW_NATIVE_AUDIO_TEST") != "1" {
		t.Skip("requires an isolated PulseAudio / PipeWire-Pulse test session")
	}
	c, err := pulse.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	name := fmt.Sprintf("superflow_go_test_%d", os.Getpid())
	var module proto.LoadModuleReply
	if err = c.RawRequest(&proto.LoadModule{Name: "module-null-sink", Args: "sink_name=" + name + " rate=48000 channels=2"}, &module); err != nil {
		t.Fatal(err)
	}
	defer c.RawRequest(&proto.UnloadModule{ModuleIndex: module.ModuleIndex}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	m := &meter{ready: make(chan struct{})}
	done := make(chan error, 2)
	go func() { done <- NativePlayback(ctx, r, name) }()
	go func() { done <- NativeCapture(ctx, m, name, true) }()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		clock := time.NewTicker(5 * time.Millisecond)
		defer clock.Stop()
		var frame int
		var b [960]byte
		for {
			select {
			case <-ctx.Done():
				return
			case <-clock.C:
				for f := 0; f < 240; f++ {
					v := int16(5000 * math.Sin(2*math.Pi*523.25*float64(frame)/48000))
					frame++
					binary.LittleEndian.PutUint16(b[f*4:], uint16(v))
					binary.LittleEndian.PutUint16(b[f*4+2:], uint16(v))
				}
				if _, err := w.Write(b[:]); err != nil {
					return
				}
			}
		}
	}()
	select {
	case err := <-done:
		t.Fatalf("native audio stopped early: %v", err)
	case <-ctx.Done():
		t.Fatal("capture timed out")
	case <-m.ready:
	}
	m.mu.Lock()
	rms := math.Sqrt(m.power / float64(m.frames))
	frames := m.frames
	m.mu.Unlock()
	if rms < 500 {
		t.Fatalf("silent loopback: RMS %.1f", rms)
	}
	t.Logf("pure Go playback → PulseAudio null sink → pure Go capture: %d frames, RMS %.1f", frames, rms)
	cancel()
	w.Close()
	r.Close()
	<-writerDone
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("native audio failed to stop")
		}
	}
}
