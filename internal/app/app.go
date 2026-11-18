package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/clipboard"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/i18n"
	"superflow.local/superflow/internal/input"
	"superflow.local/superflow/internal/transport"
	"superflow.local/superflow/internal/version"
	"superflow.local/superflow/internal/wire"
)

type App struct {
	// Optional local integration hooks. Set before Run; they never bypass TLS.
	ResolveServer                    func(pin, fallback string) string
	LayoutChanges                    chan []config.Screen
	ClipboardBackend                 clipboard.Backend
	gains                            map[string]float64
	config                           *config.Config
	mu                               sync.Mutex
	peers                            map[string]*transport.Session
	router                           *input.Router
	receiver                         *input.Receiver
	driver                           input.Driver
	clip                             *clipboard.Service
	audio                            *audio.Manager
	ran                              atomic.Bool
	focus                            atomic.Value
	status, lastError, listenAddress string
	started                          time.Time
	canControl                       bool
}

func New(c *config.Config) (*App, error) {
	if c == nil {
		return nil, errors.New("missing configuration")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Mode == "node" && c.Server == "" {
		return nil, errors.New(i18n.T("This device is not paired. Run pair first."))
	}
	a := &App{config: c.Clone(), peers: map[string]*transport.Session{}, status: "stopped", gains: map[string]float64{}}
	for name, gain := range c.Audio.Gains {
		a.gains[name] = gain
	}
	a.focus.Store(c.Name)
	return a, nil
}
func Run(ctx context.Context, c *config.Config) error {
	a, err := New(c)
	if err != nil {
		return err
	}
	return a.Run(ctx)
}
func (a *App) Run(ctx context.Context) (result error) {
	if !a.ran.CompareAndSwap(false, true) {
		return errors.New("runtime cannot be started twice")
	}
	c := a.config
	a.mu.Lock()
	a.status = "starting"
	a.started = time.Now()
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.status = "stopped"
		a.canControl = false
		if result != nil {
			a.status = "error"
			a.lastError = result.Error()
		}
		a.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.Input {
		d, err := input.New(input.Options{Devices: c.InputDevices, Width: c.ScreenWidth, Height: c.ScreenHeight, Capture: c.Mode == "hub"})
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.driver = d
		defer d.Close()
		a.receiver = input.NewReceiver(d)
		defer a.receiver.Reset()
		a.router = input.NewRouter(c.Name, c.Screens, d, func(name string) { a.focus.Store(name); slog.Info(i18n.T("Input target changed"), "device", name) })
		a.canControl = true
		a.mu.Unlock()
		defer a.router.Local()
		defer func() { a.mu.Lock(); a.canControl = false; a.mu.Unlock() }()
	}
	if c.Clipboard {
		b := a.ClipboardBackend
		if b == nil {
			var err error
			b, err = clipboard.New()
			if err != nil {
				return err
			}
		}
		a.clip = clipboard.NewService(b)
		a.clip.OnError = func(err error) { slog.Warn(i18n.T("Clipboard temporarily unavailable"), "error", err) }
		a.clip.OnChange = func(u clipboard.Update) {
			a.mu.Lock()
			defer a.mu.Unlock()
			for _, s := range a.peers {
				if s.Peer.ID != u.Source {
					s.SendClipboard(u.Text)
				}
			}
		}
	}
	if c.Audio.Codec == "opus" && (len(c.Audio.SendSystem) > 0 || len(c.Audio.SendMicrophone) > 0 || len(c.Audio.PlaySystem) > 0 || len(c.Audio.PlayMicrophone) > 0) {
		if err := audio.OpusAvailable(); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.audio = audio.NewManager(c.Audio)
	a.mu.Unlock()
	a.audio.SelectCodec = func() byte {
		a.mu.Lock()
		defer a.mu.Unlock()
		return negotiatedCodec(audio.OpusAvailable() == nil, c.Audio.Target, a.peers)
	}
	a.audio.HasTarget = func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		for name := range a.peers {
			if c.Audio.Target == "" || c.Audio.Target == name {
				return true
			}
		}
		return false
	}
	a.audio.OnError = func(err error) { slog.Warn(i18n.T("Audio decoding failed"), "error", err) }
	a.audio.Send = func(b []byte) {
		a.mu.Lock()
		defer a.mu.Unlock()
		for name, s := range a.peers {
			if c.Audio.Target == "" || c.Audio.Target == name {
				s.SendAudio(b)
			}
		}
	}
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
	run(func() error { return a.audio.Run(ctx) })
	if a.clip != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.clip.Run(ctx) }()
	}
	if c.Input && c.Mode == "hub" {
		run(func() error {
			err := a.driver.Capture(ctx, a.router.Handle)
			if err == nil && ctx.Err() == nil {
				return errors.New(i18n.T("Local input capture has stopped"))
			}
			return err
		})
	}
	if c.Mode == "hub" {
		run(func() error { return a.hub(ctx) })
	} else {
		a.mu.Lock()
		a.status = "connecting"
		a.mu.Unlock()
		run(func() error { return a.node(ctx) })
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	// Cancellation closes connections before native drivers and their state are released.
	cancel()
	a.mu.Lock()
	a.canControl = false
	a.status = "stopping"
	for _, s := range a.peers {
		s.Close()
	}
	a.mu.Unlock()
	wg.Wait()
	return err
}

func (a *App) register(s *transport.Session) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.peers) >= 16 {
		return errors.New("device limit reached")
	}
	if s.Peer.Name == a.config.Name || s.Peer.ID == a.config.ID {
		return errors.New("peer duplicates local device identity")
	}
	for _, old := range a.peers {
		if old.Peer.ID == s.Peer.ID || old.Peer.Name == s.Peer.Name {
			return errors.New("device name or ID already online")
		}
	}
	a.peers[s.Peer.Name] = s
	if a.config.AutoLayout && a.config.PlaceScreen(s.Peer.Name) {
		if a.router != nil {
			a.router.SetScreens(a.config.Screens)
		}
		if a.LayoutChanges != nil {
			screens := append([]config.Screen(nil), a.config.Screens...)
			select {
			case a.LayoutChanges <- screens:
			default:
			}
		}
	}
	if gain, ok := a.gains[s.Peer.Name]; ok {
		a.audio.SetGain(s.Peer.ID, gain)
	}
	a.status = "connected"
	a.lastError = ""
	if a.router != nil && a.config.Mode == "hub" {
		a.router.Add(s.Peer.Name, s)
	}
	slog.Info(i18n.T("Device connected"), "name", s.Peer.Name, "address", s.RemoteAddr())
	return nil
}
func (a *App) serve(ctx context.Context, s *transport.Session) {
	defer s.Close()
	if err := a.register(s); err != nil {
		slog.Warn(i18n.T("Device connection rejected"), "error", err)
		return
	}
	defer func() {
		a.mu.Lock()
		delete(a.peers, s.Peer.Name)
		if len(a.peers) == 0 {
			if a.config.Mode == "hub" {
				a.status = "listening"
			} else {
				a.status = "connecting"
			}
		}
		if a.router != nil {
			a.router.Remove(s.Peer.Name)
		}
		a.mu.Unlock()
		if a.receiver != nil {
			_ = a.receiver.Reset()
		}
		a.audio.Remove(s.Peer.ID)
		slog.Info(i18n.T("Device disconnected. Releasing remote input."), "name", s.Peer.Name)
	}()
	apply := func(e wire.Event) {
		if a.config.Mode != "node" {
			s.Close()
			return
		}
		if a.receiver != nil {
			if err := a.receiver.Apply(e); err != nil {
				slog.Error(i18n.T("Input injection failed"), "error", err)
				s.Close()
			}
		}
	}
	err := s.Run(ctx, transport.Handlers{
		Input: apply,
		Clipboard: func(text string) {
			if a.clip != nil {
				a.clip.Push(clipboard.Update{Source: s.Peer.ID, Text: text})
			}
		},
		Datagram: func(b []byte) {
			switch b[0] {
			case wire.DatagramMotion:
				if a.config.Mode == "node" && a.receiver != nil {
					if e, err := wire.ParseMotion(b); err == nil {
						apply(e)
					}
				}
			case wire.DatagramAudio:
				a.audio.Receive(s.Peer.ID, b)
			}
		},
	})
	if err != nil && ctx.Err() == nil {
		slog.Debug(i18n.T("Session ended"), "error", err)
	}
}
func (a *App) hub(ctx context.Context) error {
	t, err := transport.ServerTLS(a.config)
	if err != nil {
		return err
	}
	l, err := quic.ListenAddr(a.config.Listen, t, transport.Settings())
	if err != nil {
		return err
	}
	defer l.Close()
	a.mu.Lock()
	a.listenAddress = l.Addr().String()
	if len(a.peers) == 0 {
		a.status = "listening"
	}
	a.mu.Unlock()
	slog.Info(i18n.T("SuperFlow started"), "name", a.config.Name, "listen", l.Addr().String())
	var wg sync.WaitGroup
	defer wg.Wait()
	// Limit authenticated-session setup work as well as established peers.
	slots := make(chan struct{}, 32)
	for {
		conn, err := l.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.CloseWithError(2, "server busy")
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			s, err := transport.Accept(ctx, conn, a.config)
			if err != nil {
				_ = conn.CloseWithError(1, "authentication failed")
				return
			}
			a.serve(ctx, s)
		}()
	}
}
func (a *App) node(ctx context.Context) error {
	backoff := 500 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return nil
		}
		c := a.connectionConfig()
		s, err := transport.DialAny(ctx, c)
		if err == nil {
			backoff = 500 * time.Millisecond
			a.serve(ctx, s)
		} else {
			a.mu.Lock()
			a.lastError = err.Error()
			a.mu.Unlock()
			slog.Warn(i18n.T("Connection failed. Retrying shortly."), "server", a.config.Server, "error", err)
		}
		delay := backoff + time.Duration(rand.IntN(250))*time.Millisecond
		clock := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			clock.Stop()
			return nil
		case <-clock.C:
		}
		backoff = min(8*time.Second, backoff*2)
	}
}

func (a *App) connectionConfig() *config.Config {
	a.mu.Lock()
	c := a.config.Clone()
	a.mu.Unlock()
	if a.ResolveServer != nil {
		original := c.Server
		if address := a.ResolveServer(c.Pin, original); address != original && config.ValidAddress(address) {
			c.Server = address
			c.ServerAlternates = append([]string{original}, c.ServerAlternates...)
		}
	}
	return c
}

func negotiatedCodec(available bool, target string, peers map[string]*transport.Session) byte {
	if !available {
		return wire.PCM
	}
	for name, s := range peers {
		if (target == "" || target == name) && !slices.Contains(s.Peer.Codecs, "opus") {
			return wire.PCM
		}
	}
	return wire.Opus
}

const Release = version.Release

func Version() string { return fmt.Sprintf("SuperFlow %s (protocol %d)", Release, wire.Version) }
