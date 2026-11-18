package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/wire"
)

type sourceKey struct {
	Peer    string
	Channel byte
}
type Manager struct {
	config      config.Audio
	mu          sync.Mutex
	buffers     map[sourceKey]*Buffer
	gains       map[string]float64
	failed      map[sourceKey]byte
	latest      map[sourceKey]uint64
	sequences   [2]atomic.Uint64
	SelectCodec func() byte
	Send        func([]byte)
	HasTarget   func() bool
	OnError     func(error)
}

func NewManager(c config.Audio) *Manager {
	return &Manager{config: c, buffers: map[sourceKey]*Buffer{}, gains: map[string]float64{}, failed: map[sourceKey]byte{}, latest: map[sourceKey]uint64{}}
}
func (m *Manager) Receive(peer string, b []byte) {
	channel, codec, seq, payload, err := wire.ParseAudio(b)
	if err != nil {
		return
	}
	if channel == wire.SystemAudio && len(m.config.PlaySystem) == 0 || channel == wire.Microphone && len(m.config.PlayMicrophone) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sourceKey{peer, channel}
	if m.failed[key] == codec {
		return
	}
	buf := m.buffers[key]
	if buf != nil && buf.codec.Kind != codec {
		if seq <= m.latest[key] {
			return
		}
		buf.Close()
		delete(m.buffers, key)
		buf = nil
	}
	if buf == nil {
		if len(m.buffers) >= 32 {
			return
		}
		buf, err = NewBuffer(codec, m.config.BufferMS)
		if err != nil {
			m.failed[key] = codec
			if m.OnError != nil {
				m.OnError(err)
			}
			return
		}
		m.buffers[key] = buf
	}
	if seq > m.latest[key] {
		m.latest[key] = seq
	}
	buf.Insert(seq, payload, time.Now())
}
func (m *Manager) Remove(peer string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.gains, peer)
	for key := range m.latest {
		if key.Peer == peer {
			delete(m.latest, key)
		}
	}
	for key := range m.failed {
		if key.Peer == peer {
			delete(m.failed, key)
		}
	}
	for k, b := range m.buffers {
		if k.Peer == peer {
			delete(m.buffers, k)
			b.Close()
		}
	}
}
func command(ctx context.Context, args []string) (*exec.Cmd, error) {
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("empty PCM command")
	}
	args = append([]string(nil), args...)
	if args[0] == "@self" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		args[0] = exe
	}
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.WaitDelay = 2 * time.Second
	c.Stderr = processLog{}
	return c, nil
}

type processLog struct{}

func (processLog) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if len(text) > 2048 {
		text = text[:2048]
	}
	if text != "" {
		slog.Warn("音频设备接口", "detail", text)
	}
	return len(p), nil
}
func (m *Manager) sendCodec() byte {
	kind := byte(wire.PCM)
	if m.config.Codec == "auto" && m.SelectCodec != nil {
		return m.SelectCodec()
	}
	if m.config.Codec == "opus" || m.config.Codec == "auto" && OpusAvailable() == nil {
		kind = wire.Opus
	}
	return kind
}
func (m *Manager) capture(ctx context.Context, args []string, channel byte, kind byte) error {
	codec, err := NewCodec(kind, true)
	if err != nil {
		return err
	}
	defer codec.Close()
	c, err := command(ctx, args)
	if err != nil {
		return err
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	if err = c.Start(); err != nil {
		return err
	}
	pcm := make([]byte, codec.Frames*4)
	for {
		_, err = io.ReadFull(out, pcm)
		if err != nil {
			break
		}
		var payload []byte
		payload, err = codec.Encode(pcm)
		if err != nil {
			break
		}
		var b []byte
		seq := m.sequences[channel-1].Add(1) - 1
		b, err = wire.Audio(channel, kind, seq, payload)
		if err != nil {
			break
		}
		if m.Send != nil {
			m.Send(b)
		}
	}
	_ = out.Close()
	if c.Process != nil {
		_ = c.Process.Kill()
	}
	waitErr := c.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return errors.Join(fmt.Errorf("audio capture stopped: %w", err), waitErr)
}

// Capture is armed by explicit configuration, and runs only while a target is online.
func (m *Manager) captureConnected(ctx context.Context, args []string, channel byte) error {
	clock := time.NewTicker(200 * time.Millisecond)
	defer clock.Stop()
	for {
		for m.HasTarget != nil && !m.HasTarget() {
			select {
			case <-ctx.Done():
				return nil
			case <-clock.C:
			}
		}
		err, finished := m.captureUntilDisconnect(ctx, args, channel, clock.C)
		if finished {
			return err
		}
	}
}
func (m *Manager) captureUntilDisconnect(ctx context.Context, args []string, channel byte, tick <-chan time.Time) (error, bool) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	kind := m.sendCodec()
	go func() { done <- m.capture(child, args, channel, kind) }()
	for {
		select {
		case err := <-done:
			return err, true
		case <-ctx.Done():
			cancel()
			<-done
			return nil, true
		case <-tick:
			if m.HasTarget != nil && !m.HasTarget() || m.sendCodec() != kind {
				cancel()
				<-done
				return nil, false
			}
		}
	}
}
func (m *Manager) play(ctx context.Context, args []string, queue <-chan []byte) error {
	c, err := command(ctx, args)
	if err != nil {
		return err
	}
	in, err := c.StdinPipe()
	if err != nil {
		return err
	}
	if err = c.Start(); err != nil {
		return err
	}
	defer func() {
		in.Close()
		if c.Process != nil {
			_ = c.Process.Kill()
		}
		_ = c.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case b := <-queue:
			if _, err = in.Write(b); err != nil {
				return err
			}
		}
	}
}
func (m *Manager) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := fn()
			if ctx.Err() == nil {
				select {
				case errs <- err:
				default:
				}
			}
		}()
	}
	var queues [2]chan []byte
	for i, args := range [][]string{m.config.PlaySystem, m.config.PlayMicrophone} {
		if len(args) > 0 {
			queues[i] = make(chan []byte, 4)
			run(func() error { return m.play(ctx, args, queues[i]) })
		}
	}
	for i, args := range [][]string{m.config.SendSystem, m.config.SendMicrophone} {
		if len(args) > 0 {
			run(func() error { return m.captureConnected(ctx, args, byte(i+1)) })
		}
	}
	var ticks <-chan time.Time
	if queues[0] != nil || queues[1] != nil {
		clock := time.NewTicker(5 * time.Millisecond)
		defer clock.Stop()
		ticks = clock.C
	}
	var result error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case result = <-errs:
			break loop
		case now := <-ticks:
			var blocks [2][][]byte
			var gains [2][]float64
			m.mu.Lock()
			for k, b := range m.buffers {
				blocks[k.Channel-1] = append(blocks[k.Channel-1], b.Read(now))
				gain := 1.0
				if g, ok := m.gains[k.Peer]; ok && k.Channel == 1 {
					gain = g
				}
				gains[k.Channel-1] = append(gains[k.Channel-1], gain)
			}
			m.mu.Unlock()
			for i, q := range queues {
				if q == nil {
					continue
				}
				pcm := MixWeighted(blocks[i], gains[i])
				select {
				case q <- pcm:
				default:
					select {
					case <-q:
					default:
					}
					select {
					case q <- pcm:
					default:
					}
				}
			}
		}
	}
	cancel()
	wg.Wait()
	m.mu.Lock()
	for k, b := range m.buffers {
		b.Close()
		delete(m.buffers, k)
	}
	m.mu.Unlock()
	return result
}
