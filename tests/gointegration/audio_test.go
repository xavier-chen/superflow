//go:build integration

package gointegration

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"superflow.local/superflow/internal/app"
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/config"
)

type report struct {
	Frames int
	RMS    float64
	Power  map[string]float64
}

// These helpers run as isolated Go subprocesses. No actual microphone or speaker opens.
func TestPCMHelper(t *testing.T) {
	index := -1
	for i, a := range os.Args {
		if a == "sf-tone" || a == "sf-sink" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Skip("subprocess helper")
	}
	if os.Args[index] == "sf-tone" {
		freq, _ := strconv.ParseFloat(os.Args[index+1], 64)
		clock := time.NewTicker(5 * time.Millisecond)
		defer clock.Stop()
		var frame uint64
		var b [960]byte
		for range clock.C {
			for f := 0; f < 240; f++ {
				v := int16(6000 * math.Sin(2*math.Pi*freq*float64(frame)/48000))
				frame++
				binary.LittleEndian.PutUint16(b[f*4:], uint16(v))
				binary.LittleEndian.PutUint16(b[f*4+2:], uint16(v))
			}
			if _, err := os.Stdout.Write(b[:]); err != nil {
				os.Exit(0)
			}
		}
	} else {
		path := os.Args[index+1]
		r := report{Power: map[string]float64{}}
		window := make([]float64, 0, 4800)
		var b [960]byte
		var squares float64
		for {
			if _, err := io.ReadFull(os.Stdin, b[:]); err != nil {
				os.Exit(0)
			}
			for f := 0; f < 240; f++ {
				v := float64(int16(binary.LittleEndian.Uint16(b[f*4:])))
				window = append(window, v)
				squares += v * v
				r.Frames++
			}
			if len(window) < 4800 {
				continue
			}
			for _, freq := range []float64{440, 660, 880} {
				var re, im float64
				for i, v := range window {
					a := 2 * math.Pi * freq * float64(i) / 48000
					re += v * math.Cos(a)
					im += v * math.Sin(a)
				}
				r.Power[fmt.Sprint(freq)] += (re*re + im*im) / float64(len(window)*len(window))
			}
			r.RMS = math.Sqrt(squares / float64(r.Frames))
			data, _ := json.Marshal(r)
			_ = os.WriteFile(path+".tmp", data, 0600)
			_ = os.Rename(path+".tmp", path)
			window = window[:0]
		}
	}
}

func TestThreeAudioChannelsAcrossQUIC(t *testing.T) {
	for _, codec := range []string{"pcm", "opus", "auto"} {
		t.Run(codec, func(t *testing.T) {
			if codec == "opus" {
				if err := audio.OpusAvailable(); err != nil {
					t.Skip(err)
				}
			}
			port, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := port.LocalAddr().String()
			port.Close()
			h, err := config.New("hub", "hub")
			if err != nil {
				t.Fatal(err)
			}
			h.Listen = address
			h.Input = false
			n, err := config.New("node", "node")
			if err != nil {
				t.Fatal(err)
			}
			code, _ := h.Code()
			if err = n.Pair(code, address); err != nil {
				t.Fatal(err)
			}
			n.Input = false
			h.Audio.Codec = codec
			n.Audio.Codec = codec
			h.Audio.Target = "node"
			helper := func(mode, arg string) []string {
				return []string{os.Args[0], "-test.run=^TestPCMHelper$", "--", mode, arg}
			}
			dir := t.TempDir()
			system := filepath.Join(dir, "system.json")
			upMic := filepath.Join(dir, "up-mic.json")
			downMic := filepath.Join(dir, "down-mic.json")
			h.Audio.SendMicrophone = helper("sf-tone", "660")
			h.Audio.PlaySystem = helper("sf-sink", system)
			h.Audio.PlayMicrophone = helper("sf-sink", upMic)
			n.Audio.SendSystem = helper("sf-tone", "440")
			n.Audio.SendMicrophone = helper("sf-tone", "880")
			n.Audio.PlayMicrophone = helper("sf-sink", downMic)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			done := make(chan error, 2)
			go func() { done <- app.Run(ctx, h) }()
			go func() { done <- app.Run(ctx, n) }()
			defer func() {
				cancel()
				for i := 0; i < 2; i++ {
					select {
					case err := <-done:
						if err != nil {
							t.Error(err)
						}
					case <-time.After(3 * time.Second):
						t.Error("application shutdown leaked a worker")
					}
				}
			}()
			for _, expected := range []struct {
				path string
				freq string
			}{{system, "440"}, {upMic, "880"}, {downMic, "660"}} {
				var got report
				for {
					data, err := os.ReadFile(expected.path)
					if err == nil && json.Unmarshal(data, &got) == nil && got.Frames >= 48000 && got.RMS > 1000 {
						break
					}
					select {
					case err := <-done:
						t.Fatalf("app stopped early: %v", err)
					case <-ctx.Done():
						t.Fatalf("audio missing (%s): %+v", expected.freq, got)
					case <-time.After(50 * time.Millisecond):
					}
				}
				for freq, p := range got.Power {
					if freq != expected.freq && got.Power[expected.freq] < p*4 {
						t.Fatalf("channels mixed incorrectly: expected %s, power %v", expected.freq, got.Power)
					}
				}
				t.Logf("%s: frames=%d RMS=%.1f dominant=%sHz", codec, got.Frames, got.RMS, expected.freq)
			}
		})
	}
}
