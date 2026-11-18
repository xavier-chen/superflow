package control

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"superflow.local/superflow/internal/app"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/transport"
)

func fixture(t *testing.T) (context.Context, *Service, *config.Config) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, err := config.New("hub", "hub")
	if err != nil {
		t.Fatal(err)
	}
	c.Input = false
	c.Listen = "127.0.0.1:0"
	c.Screens = append(c.Screens, config.Screen{Name: "node", X: 1})
	path := filepath.Join(t.TempDir(), "v2.json")
	if err = config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	return ctx, s, c
}
func wait(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("state transition timed out")
}
func TestApplyRestartsWithoutReplacingIdentity(t *testing.T) {
	ctx, s, c := fixture(t)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().State == "listening" })
	next := c.Clone()
	next.Audio.BufferMS = 60
	if err := s.Apply(ctx, c, next); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().State == "listening" })
	got, err := config.Load(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != c.ID || got.Secret != c.Secret || got.PrivateKey != c.PrivateKey || got.Audio.BufferMS != 60 {
		t.Fatal("settings replaced pairing or failed to apply")
	}
	b, _ := json.Marshal(s.Snapshot())
	for _, secret := range []string{c.Secret, c.PrivateKey, c.Certificate} {
		if strings.Contains(string(b), secret) {
			t.Fatal("status exposes pairing material")
		}
	}
}
func TestSaveConflictKeepsExistingFileAndRunningService(t *testing.T) {
	ctx, s, c := fixture(t)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().State == "listening" })
	external := c.Clone()
	external.Audio.BufferMS = 80
	if err := config.Replace(s.Path(), c, external); err != nil {
		t.Fatal(err)
	}
	edit := c.Clone()
	edit.Audio.BufferMS = 120
	if err := s.Apply(ctx, c, edit); !errors.Is(err, config.ErrChanged) {
		t.Fatal("lost external changes", err)
	}
	wait(t, func() bool { return s.Snapshot().State == "listening" })
	got, err := config.Load(s.Path())
	if err != nil || got.Audio.BufferMS != 80 {
		t.Fatal("external settings overwritten", err)
	}
}
func TestLiveStatusAndGainSurviveReconnect(t *testing.T) {
	ctx, s, c := fixture(t)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().ListenAddress != "" })
	n, err := config.New("node", "node")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := c.Code()
	if err = n.Pair(code, s.Snapshot().ListenAddress); err != nil {
		t.Fatal(err)
	}
	n.Input = false
	connect := func() (context.CancelFunc, chan error) {
		peerCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- app.Run(peerCtx, n) }()
		return cancel, done
	}
	cancel, done := connect()
	wait(t, func() bool { return len(s.Snapshot().Peers) == 1 })
	if err = s.SetGain("node", .5); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Peers[0].Gain != .5 {
		t.Fatal("live gain not applied")
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return len(s.Snapshot().Peers) == 0 })
	if err = s.SetGain("node", .4); err != nil {
		t.Fatal(err)
	}
	cancel, done = connect()
	wait(t, func() bool { return len(s.Snapshot().Peers) == 1 })
	if s.Snapshot().Peers[0].Gain != .4 {
		t.Fatal("reconnect lost saved volume")
	}
	cancel()
	<-done
}

func TestAuthenticatedDeviceIsAutomaticallyPlacedAndSaved(t *testing.T) {
	ctx, s, c := fixture(t)
	next := c.Clone()
	next.AutoLayout = true
	if err := s.Apply(ctx, c, next); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().ListenAddress != "" })
	node, _ := config.New("node", "new-device")
	code, _ := next.Code()
	if err := node.Pair(code, s.Snapshot().ListenAddress); err != nil {
		t.Fatal(err)
	}
	node.Input = false
	bad := node.Clone()
	bad.Name = "untrusted"
	bad.Secret = strings.Repeat("x", 43)
	if session, err := transport.Dial(ctx, bad); err == nil {
		session.Close()
		t.Fatal("invalid invitation was accepted")
	}
	saved, _ := config.Load(s.Path())
	for _, screen := range saved.Screens {
		if screen.Name == bad.Name {
			t.Fatal("untrusted announcement changed layout")
		}
	}
	peerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(peerCtx, node) }()
	wait(t, func() bool { saved, err := config.Load(s.Path()); return err == nil && len(saved.Screens) == 3 })
	saved, _ = config.Load(s.Path())
	if saved.Screens[1].Name != "node" || saved.Screens[1].X != 1 || saved.Screens[2].Name != "new-device" || saved.Screens[2].X != 2 {
		t.Fatal("automatic placement lost existing arrangement", saved.Screens)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
