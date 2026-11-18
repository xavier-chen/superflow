//go:build linux

package audio

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

func Devices() ([]Device, error) {
	c, err := pulse.NewClient(pulse.ClientApplicationName("SuperFlow"))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	sinks, err := c.ListSinks()
	if err != nil {
		return nil, err
	}
	sources, err := c.ListSources()
	if err != nil {
		return nil, err
	}
	ds, _ := c.DefaultSink()
	dr, _ := c.DefaultSource()
	var out []Device
	for _, s := range sinks {
		out = append(out, Device{"playback", s.ID(), s.Name(), ds != nil && ds.ID() == s.ID()})
	}
	for _, s := range sources {
		out = append(out, Device{"capture", s.ID(), s.Name(), dr != nil && dr.ID() == s.ID()})
	}
	return out, nil
}

type ringWriter struct{ ring *Ring }

func (w ringWriter) Write(b []byte) (int, error) { w.ring.Write(b); return len(b), nil }
func NativeCapture(ctx context.Context, w io.Writer, id string, loopback bool) error {
	c, err := pulse.NewClient(pulse.ClientApplicationName("SuperFlow capture"))
	if err != nil {
		return err
	}
	defer c.Close()
	opts := []pulse.RecordOption{pulse.RecordStereo, pulse.RecordSampleRate(48000), pulse.RecordLatency(.01), pulse.RecordMediaName("SuperFlow capture")}
	if loopback {
		var sink *pulse.Sink
		if id == "" {
			sink, err = c.DefaultSink()
		} else {
			sink, err = c.SinkByID(id)
		}
		if err != nil {
			return err
		}
		opts = append(opts, pulse.RecordMonitor(sink))
	} else if id != "" {
		source, err := c.SourceByID(id)
		if err != nil {
			return err
		}
		opts = append(opts, pulse.RecordSource(source))
	}
	ring := NewRing(19200)
	s, err := c.NewRecord(pulse.NewWriter(ringWriter{ring}, proto.FormatInt16LE), opts...)
	if err != nil {
		return err
	}
	defer s.Close()
	s.Start()
	clock := time.NewTicker(2 * time.Millisecond)
	defer clock.Stop()
	var b [1920]byte
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-clock.C:
			for {
				n := ring.Read(b[:])
				if n == 0 {
					break
				}
				if _, err = w.Write(b[:n]); err != nil {
					return err
				}
			}
		}
	}
}

type endingReader struct {
	reader  io.Reader
	done    chan error
	once    sync.Once
	pending error
}

func (r *endingReader) Read(b []byte) (int, error) {
	if r.pending != nil {
		r.once.Do(func() { r.done <- r.pending })
		return 0, pulse.EndOfData
	}
	n, err := r.reader.Read(b)
	if err != nil {
		r.pending = err
		if n > 0 {
			return n, nil
		}
		r.once.Do(func() { r.done <- err })
		return 0, pulse.EndOfData
	}
	return n, nil
}
func NativePlayback(ctx context.Context, r io.Reader, id string) error {
	c, err := pulse.NewClient(pulse.ClientApplicationName("SuperFlow playback"))
	if err != nil {
		return err
	}
	defer c.Close()
	opts := []pulse.PlaybackOption{pulse.PlaybackStereo, pulse.PlaybackSampleRate(48000), pulse.PlaybackLatency(.02)}
	if id != "" {
		sink, err := c.SinkByID(id)
		if err != nil {
			return err
		}
		opts = append(opts, pulse.PlaybackSink(sink))
	}
	reader := &endingReader{reader: r, done: make(chan error, 1)}
	s, err := c.NewPlayback(pulse.NewReader(reader, proto.FormatInt16LE), opts...)
	if err != nil {
		return err
	}
	defer s.Close()
	started := make(chan struct{})
	go func() { s.Start(); close(started) }()
	select {
	case <-ctx.Done():
		if rc, ok := r.(io.Closer); ok {
			_ = rc.Close()
		}
		c.Close()
		return nil
	case <-started:
	}
	select {
	case <-ctx.Done():
		if rc, ok := r.(io.Closer); ok {
			_ = rc.Close()
		}
		c.Close()
		return nil
	case err := <-reader.done:
		if errors.Is(err, io.EOF) || ctx.Err() != nil {
			return nil
		}
		return err
	}
}
