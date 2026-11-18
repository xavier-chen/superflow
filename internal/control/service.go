// Package control owns the local application's start/stop and explicit settings actions.
package control

import (
	"context"
	"errors"
	"math"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"

	"superflow.local/superflow/internal/app"
	"superflow.local/superflow/internal/clipboard"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/discovery"
	"superflow.local/superflow/internal/i18n"
)

type Service struct {
	op        sync.Mutex
	mu        sync.Mutex
	ctx       context.Context
	path      string
	config    *config.Config
	runtime   *app.App
	cancel    context.CancelFunc
	done      chan struct{}
	lastError string
	browser   *discovery.Browser
	clipboard clipboard.Backend
}

func New(ctx context.Context, path string) (*Service, error) {
	c, err := config.LoadCompatible(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return &Service{ctx: ctx, path: path, config: c}, nil
}
func (s *Service) Config() *config.Config { s.mu.Lock(); defer s.mu.Unlock(); return s.config.Clone() }
func (s *Service) Path() string           { return s.path }
func (s *Service) running() bool {
	if s.done == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
func (s *Service) Start() error { s.op.Lock(); defer s.op.Unlock(); return s.start() }
func (s *Service) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if s.config == nil {
		return errors.New(i18n.T("Create a configuration first"))
	}
	if s.running() {
		return errors.New(i18n.T("SuperFlow is already running"))
	}
	r, err := app.New(s.config)
	if err != nil {
		return err
	}
	if s.browser != nil {
		r.ResolveServer = s.browser.Resolve
	}
	r.LayoutChanges = make(chan []config.Screen, 16)
	r.ClipboardBackend = s.clipboard
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.runtime = r
	s.cancel = cancel
	s.done = done
	s.lastError = ""
	go s.rememberLayouts(ctx, r)
	go func() {
		defer close(done)
		err := r.Run(ctx)
		cancel()
		s.mu.Lock()
		if s.runtime == r && err != nil {
			s.lastError = err.Error()
		}
		s.mu.Unlock()
	}()
	return nil
}

// EnableDiscovery is opt-in at the desktop entry point; unit tests and core
// configuration readers never open LAN sockets as a side effect.
func (s *Service) EnableDiscovery() error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.browser != nil {
		return nil
	}
	b, err := discovery.Start(s.ctx, func() discovery.Announcement {
		c := s.Config()
		if c == nil {
			return discovery.Announcement{}
		}
		a := discovery.Announcement{Protocol: "superflow/2", ID: c.ID, Name: c.Name, OS: runtime.GOOS, Pin: c.Pin}
		if c.Mode != "hub" {
			a.Pin = ""
			return a
		}
		state := s.Snapshot()
		address := c.Listen
		if state.ListenAddress != "" {
			address = state.ListenAddress
		}
		_, port, _ := net.SplitHostPort(address)
		a.Port, _ = strconv.Atoi(port)
		a.Ready = state.State == "listening" || state.State == "connected"
		return a
	})
	if err != nil {
		return err
	}
	s.browser = b
	return nil
}
func (s *Service) Nearby() []discovery.Peer {
	s.mu.Lock()
	b := s.browser
	s.mu.Unlock()
	if b == nil {
		return nil
	}
	return b.Snapshot()
}
func (s *Service) Discover() {
	s.mu.Lock()
	b := s.browser
	s.mu.Unlock()
	if b != nil {
		b.Refresh()
	}
}
func (s *Service) rememberLayouts(ctx context.Context, r *app.App) {
	for {
		select {
		case <-ctx.Done():
			return
		case screens := <-r.LayoutChanges:
			s.op.Lock()
			s.mu.Lock()
			active := s.runtime == r && s.running()
			s.mu.Unlock()
			if active {
				expected := s.Config()
				next := expected.Clone()
				changed := false
				if next.AutoLayout {
					for _, screen := range screens {
						if next.PlaceScreen(screen.Name) {
							changed = true
						}
					}
				}
				if changed {
					if err := config.Replace(s.path, expected, next); err == nil {
						s.mu.Lock()
						s.config = next
						s.mu.Unlock()
					} else {
						s.mu.Lock()
						s.lastError = i18n.T("Could not save automatic layout: ") + err.Error()
						s.mu.Unlock()
					}
				}
			}
			s.op.Unlock()
		}
	}
}
func (s *Service) Stop(ctx context.Context) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.stop(ctx)
}
func (s *Service) stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) Snapshot() app.Status {
	s.mu.Lock()
	r, c, err := s.runtime, s.config, s.lastError
	s.mu.Unlock()
	if r != nil {
		state := r.Snapshot()
		if err != "" {
			state.LastError = err
		}
		return state
	}
	state := "stopped"
	if c == nil {
		state = "unconfigured"
		return app.Status{State: state, LastError: err}
	}
	return app.Status{Name: c.Name, Mode: c.Mode, State: state, Target: c.Name, Input: c.Input, Clipboard: c.Clipboard, LastError: err}
}
func (s *Service) Select(name string) error {
	s.mu.Lock()
	r := s.runtime
	s.mu.Unlock()
	if r == nil {
		return errors.New(i18n.T("Start the host first"))
	}
	return r.Select(name)
}
func (s *Service) ReturnLocal() error {
	s.mu.Lock()
	r := s.runtime
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.ReturnLocal()
}
func (s *Service) Create(c *config.Config) error {
	s.op.Lock()
	defer s.op.Unlock()
	if c == nil {
		return errors.New("missing configuration")
	}
	if err := config.Save(s.path, c); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = c.Clone()
	s.mu.Unlock()
	return nil
}
func (s *Service) Reload() error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	running := s.running()
	s.mu.Unlock()
	if running {
		return errors.New(i18n.T("Pause sharing before reloading the configuration"))
	}
	c, err := config.LoadCompatible(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.config = c
	s.runtime = nil
	s.lastError = ""
	s.mu.Unlock()
	return nil
}
func (s *Service) Apply(ctx context.Context, expected, next *config.Config) error {
	if next == nil || expected == nil {
		return errors.New("missing settings")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if next.ID != expected.ID || next.Secret != expected.Secret || next.Pin != expected.Pin || next.Certificate != expected.Certificate || next.PrivateKey != expected.PrivateKey || next.Mode != expected.Mode {
		return errors.New(i18n.T("Saving settings cannot change the device identity or role"))
	}
	s.op.Lock()
	defer s.op.Unlock()
	current := s.Config()
	merged, err := config.MergeEdit(expected, next, current)
	if err != nil {
		return err
	}
	expected, next = current, merged
	s.mu.Lock()
	running := s.running()
	s.mu.Unlock()
	if err := s.stop(ctx); err != nil {
		return err
	}
	if err := config.Replace(s.path, expected, next); err != nil {
		if running {
			return errors.Join(err, s.start())
		}
		return err
	}
	s.mu.Lock()
	s.config = next.Clone()
	s.runtime = nil
	s.lastError = ""
	s.mu.Unlock()
	if running {
		return s.start()
	}
	return nil
}
func (s *Service) SetGain(name string, gain float64) error {
	if math.IsNaN(gain) || math.IsInf(gain, 0) || gain < 0 || gain > 2 {
		return errors.New(i18n.T("Volume must be between 0 and 200%"))
	}
	s.op.Lock()
	defer s.op.Unlock()
	expected := s.Config()
	if expected == nil {
		return errors.New(i18n.T("Not configured"))
	}
	next := expected.Clone()
	if next.Audio.Gains == nil {
		next.Audio.Gains = map[string]float64{}
	}
	next.Audio.Gains[name] = gain
	if err := config.Replace(s.path, expected, next); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = next
	r := s.runtime
	s.mu.Unlock()
	if r != nil {
		_ = r.SetGain(name, gain)
	}
	return nil
}

// UseClipboard installs a native GUI adapter before sharing starts.
func (s *Service) UseClipboard(backend clipboard.Backend) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running() {
		return errors.New(i18n.T("Pause sharing before changing the clipboard backend"))
	}
	s.clipboard = backend
	return nil
}
