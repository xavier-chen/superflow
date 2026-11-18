// Package transport multiplexes independent input, clipboard and real-time lanes.
package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/wire"
)

type Hello struct {
	Version int      `json:"version"`
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Token   string   `json:"token,omitempty"`
	Codecs  []string `json:"codecs,omitempty"`
}
type Clipboard struct {
	Text string `json:"text"`
}

type Handlers struct {
	Input     func(wire.Event)
	Clipboard func(string)
	Datagram  func([]byte)
}

type Session struct {
	Peer          Hello
	conn          *quic.Conn
	input         *quic.Stream
	clipboard     *quic.Stream
	events        chan wire.Event
	clips         chan Clipboard
	motion        chan []byte
	audio         chan []byte
	DroppedAudio  atomic.Uint64
	DroppedMotion atomic.Uint64
	once          sync.Once
}

func Settings() *quic.Config {
	return &quic.Config{EnableDatagrams: true, HandshakeIdleTimeout: 5 * time.Second,
		MaxIdleTimeout: 4 * time.Second, KeepAlivePeriod: time.Second, MaxIncomingStreams: 2,
		MaxIncomingUniStreams: -1, InitialStreamReceiveWindow: 64 << 10, MaxStreamReceiveWindow: 2 << 20,
		InitialConnectionReceiveWindow: 128 << 10, MaxConnectionReceiveWindow: 4 << 20,
		Allow0RTT: false}
}

func ServerTLS(c *config.Config) (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(c.Certificate), []byte(c.PrivateKey))
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(cert.Certificate[0])
	if hex.EncodeToString(h[:]) != c.Pin {
		return nil, errors.New("certificate pin does not match hub certificate")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, NextProtos: []string{"superflow/2", "soundflow/2"}}, nil
}

func ClientTLS(pin string) (*tls.Config, error) {
	expected, err := hex.DecodeString(pin)
	if err != nil || len(expected) != 32 {
		return nil, errors.New("invalid server pin")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, NextProtos: []string{"superflow/2", "soundflow/2"},
		// The pairing code pins the entire certificate instead of a public CA / DNS name.
		InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) != 1 {
				return errors.New("unexpected certificate chain")
			}
			cert := cs.PeerCertificates[0]
			now := time.Now()
			if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
				return errors.New("hub certificate expired or not yet valid")
			}
			h := sha256.Sum256(cert.Raw)
			if subtle.ConstantTimeCompare(h[:], expected) != 1 {
				return errors.New("hub certificate fingerprint mismatch")
			}
			return nil
		}}, nil
}

func self(c *config.Config) Hello {
	codecs := []string{"pcm"}
	if audio.OpusAvailable() == nil {
		codecs = append(codecs, "opus")
	}
	return Hello{Version: wire.Version, ID: c.ID, Name: c.Name, Codecs: codecs}
}
func validate(h Hello) error {
	id, err := hex.DecodeString(h.ID)
	if h.Version != wire.Version || err != nil || len(id) != 16 || !config.ValidName(h.Name) {
		return errors.New("invalid peer hello")
	}
	if len(h.Codecs) > 2 {
		return errors.New("invalid codec capabilities")
	}
	for _, codec := range h.Codecs {
		if codec != "pcm" && codec != "opus" {
			return errors.New("invalid codec capability")
		}
	}
	return nil
}
func makeSession(conn *quic.Conn, stream, clip *quic.Stream, peer Hello) *Session {
	peer.Token = ""
	stream.SetPriority(0, false)
	clip.SetPriority(7, true)
	return &Session{Peer: peer, conn: conn, input: stream, clipboard: clip, events: make(chan wire.Event, 128),
		clips: make(chan Clipboard, 1), motion: make(chan []byte, 1), audio: make(chan []byte, 8)}
}

func Accept(ctx context.Context, conn *quic.Conn, c *config.Config) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, err := conn.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	var h Hello
	if err = wire.Read(s, &h); err != nil {
		return nil, err
	}
	if err = validate(h); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(h.Token), []byte(c.Secret)) != 1 {
		return nil, errors.New("pairing token rejected")
	}
	if err = wire.Write(s, self(c)); err != nil {
		return nil, err
	}
	clip, err := conn.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	_ = clip.SetReadDeadline(time.Now().Add(5 * time.Second))
	var marker [1]byte
	if _, err = io.ReadFull(clip, marker[:]); err != nil {
		return nil, err
	}
	if marker[0] != wire.StreamClipboard {
		return nil, errors.New("unexpected stream type")
	}
	_ = s.SetDeadline(time.Time{})
	_ = clip.SetDeadline(time.Time{})
	return makeSession(conn, s, clip, h), nil
}

func Dial(ctx context.Context, c *config.Config) (*Session, error) {
	conn, err := dialTLS(ctx, c)
	if err != nil {
		return nil, err
	}
	return authenticateClient(ctx, conn, c)
}

func dialTLS(ctx context.Context, c *config.Config) (*quic.Conn, error) {
	t, err := ClientTLS(c.Pin)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := quic.DialAddr(ctx, c.Server, t, Settings())
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func authenticateClient(ctx context.Context, conn *quic.Conn, c *config.Config) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ok := false
	defer func() {
		if !ok {
			_ = conn.CloseWithError(1, "handshake failed")
		}
	}()
	s, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	h := self(c)
	h.Token = c.Secret
	if err = wire.Write(s, h); err != nil {
		return nil, err
	}
	var peer Hello
	if err = wire.Read(s, &peer); err != nil {
		return nil, err
	}
	if err = validate(peer); err != nil {
		return nil, err
	}
	clip, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	_ = clip.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err = clip.Write([]byte{wire.StreamClipboard}); err != nil {
		return nil, err
	}
	_ = s.SetDeadline(time.Time{})
	_ = clip.SetDeadline(time.Time{})
	ok = true
	return makeSession(conn, s, clip, peer), nil
}

func (s *Session) Context() context.Context         { return s.conn.Context() }
func (s *Session) RemoteAddr() string               { return s.conn.RemoteAddr().String() }
func (s *Session) Statistics() quic.ConnectionStats { return s.conn.ConnectionStats() }
func (s *Session) Close()                           { s.once.Do(func() { _ = s.conn.CloseWithError(0, "session closed") }) }

// Never block a native input callback. Queue overflow fails closed to local control.
func (s *Session) SendInput(e wire.Event) bool {
	select {
	case <-s.conn.Context().Done():
		return false
	default:
	}
	select {
	case s.events <- e:
		return true
	default:
		s.Close()
		return false
	}
}
func (s *Session) SendClipboard(text string) {
	if len(text) > wire.MaxMessage-64 {
		return
	}
	v := Clipboard{Text: text}
	if b, err := json.Marshal(v); err != nil || len(b) > wire.MaxMessage {
		return
	}
	select {
	case s.clips <- v:
	default:
		select {
		case <-s.clips:
		default:
		}
		select {
		case s.clips <- v:
		default:
		}
	}
}
func (s *Session) SendMotion(e wire.Event) {
	b := wire.Motion(e)
	select {
	case s.motion <- b:
	default:
		s.DroppedMotion.Add(1)
		select {
		case <-s.motion:
		default:
		}
		select {
		case s.motion <- b:
		default:
		}
	}
}
func (s *Session) SendAudio(b []byte) {
	select {
	case s.audio <- b:
	default:
		s.DroppedAudio.Add(1)
		select {
		case <-s.audio:
		default:
		}
		select {
		case s.audio <- b:
		default:
		}
	}
}

// Run owns all stream goroutines and joins them before returning.
func (s *Session) Run(ctx context.Context, h Handlers) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	run := func(f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := f()
			select {
			case errs <- err:
			default:
			}
		}()
	}
	run(func() error {
		for {
			var e wire.Event
			if err := wire.Read(s.input, &e); err != nil {
				return err
			}
			if err := e.Validate(); err != nil {
				return err
			}
			if h.Input != nil {
				h.Input(e)
			}
		}
	})
	run(func() error {
		for {
			var c Clipboard
			if err := wire.Read(s.clipboard, &c); err != nil {
				return err
			}
			if len(c.Text) > wire.MaxMessage-64 {
				return errors.New("clipboard too large")
			}
			if h.Clipboard != nil {
				h.Clipboard(c.Text)
			}
		}
	})
	run(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case e := <-s.events:
				_ = s.input.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if err := wire.Write(s.input, e); err != nil {
					return err
				}
			}
		}
	})
	run(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case c := <-s.clips:
				_ = s.clipboard.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if err := wire.Write(s.clipboard, c); err != nil {
					return err
				}
			}
		}
	})
	run(func() error {
		for {
			b, err := s.conn.ReceiveDatagram(ctx)
			if err != nil {
				return err
			}
			if len(b) > 0 && len(b) <= wire.MaxDatagram && h.Datagram != nil {
				h.Datagram(b)
			}
		}
	})
	run(func() error {
		interval := time.Second / 120
		clock := time.NewTimer(interval)
		clock.Stop()
		defer clock.Stop()
		var tick <-chan time.Time
		var sent time.Time
		var latest []byte
		repeats := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case b := <-s.motion:
				latest = b
				repeats = 3
				if tick == nil {
					delay := time.Until(sent.Add(interval))
					if delay < 0 {
						delay = 0
					}
					clock.Reset(delay)
					tick = clock.C
				}
			case <-tick:
				select {
				case b := <-s.motion:
					latest = b
					repeats = 3
				default:
				}
				if repeats > 0 {
					if err := s.conn.SendDatagram(latest); err != nil {
						return err
					}
					repeats--
					sent = time.Now()
				}
				if repeats > 0 {
					clock.Reset(interval)
					tick = clock.C
				} else {
					tick = nil
				}
			case b := <-s.audio:
				if err := s.conn.SendDatagram(b); err != nil {
					return err
				}
			}
		}
	})
	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-errs:
	case <-s.conn.Context().Done():
		err = context.Cause(s.conn.Context())
	}
	cancel()
	s.Close()
	wg.Wait()
	if err != nil {
		return fmt.Errorf("peer %s: %w", s.Peer.Name, err)
	}
	return nil
}
