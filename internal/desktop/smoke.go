//go:build desktop && smoke

package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"superflow.local/superflow/internal/brand"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/control"
	"superflow.local/superflow/internal/i18n"
	"superflow.local/superflow/internal/instance"
)

// CheckNative runs the real graphics and tray drivers. It requires an explicit
// isolated-session flag, never captures input/audio, and uses only a loopback
// sharing listener and a temporary configuration.
func CheckNative(parent context.Context, output string, withTray, checkClipboard bool, background ...bool) error {
	if os.Getenv("SUPERFLOW_NATIVE_DESKTOP_TEST") != "1" {
		return errors.New("native desktop checks require SUPERFLOW_NATIVE_DESKTOP_TEST=1 in an isolated session")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	watcher, err := testWatcher(withTray)
	if err != nil {
		return err
	}
	if watcher != nil {
		defer watcher.Close()
	}
	root, err := os.MkdirTemp("", "superflow-native-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	setLanguage(i18n.System)
	c, err := config.New("hub", tr("This computer"))
	if err != nil {
		return err
	}
	c.Input = false
	c.Clipboard = false
	c.Listen = "127.0.0.1:0"
	c.Screens = append(c.Screens, config.Screen{Name: tr("Workstation"), X: 1})
	path := filepath.Join(root, "v2.json")
	if err = config.Save(path, c); err != nil {
		return err
	}
	service, err := control.New(ctx, path)
	if err != nil {
		return err
	}
	guard, err := instance.Open(ctx, path)
	if err != nil || guard == nil {
		return fmt.Errorf("native instance check: %w", err)
	}
	defer guard.Close()
	prepareNativeProcess()
	a := app.NewWithID(brand.ApplicationID + ".nativecheck")
	a.Settings().SetTheme(superTheme{})
	a.SetIcon(fyne.NewStaticResource("SuperFlow.svg", []byte(brand.SVG)))
	g := newGUI(ctx, cancel, a, service)
	g.canRecover = true
	g.audioLoaded = true
	g.initializeTray()
	g.window.SetCloseIntercept(g.closeWindow)
	a.Lifecycle().SetOnEnteredForeground(func() { fyne.Do(g.activated) })
	started := make(chan struct{})
	a.Lifecycle().SetOnStarted(func() { close(started) })
	if err = service.Start(); err != nil {
		return err
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = service.Stop(stop)
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-guard.Activations:
				fyne.Do(g.reveal)
			}
		}
	}()
	result := make(chan error, 1)
	go func() {
		select {
		case <-started:
		case <-ctx.Done():
			result <- ctx.Err()
			return
		}
		err := g.nativeChecks(ctx, output, path, watcher, checkClipboard)
		result <- err
		fyne.Do(g.quit)
	}()
	if len(background) > 0 && background[0] {
		g.visible.Store(false)
	} else {
		g.window.Show()
	}
	a.Run()
	select {
	case err := <-result:
		return err
	default:
		return errors.New("native window exited before the check completed")
	}
}
func (g *gui) nativeChecks(ctx context.Context, output, path string, watcher nativeWatcher, checkClipboard bool) error {
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	wait := func(predicate func() bool) error {
		timer := time.NewTicker(20 * time.Millisecond)
		defer timer.Stop()
		for {
			if predicate() {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if err := wait(func() bool { return g.service.Snapshot().State == "listening" }); err != nil {
		return err
	}
	if !g.visible.Load() {
		forwarded, err := instance.Open(ctx, path)
		if err != nil {
			return err
		}
		if forwarded != nil {
			forwarded.Close()
			return errors.New("background startup could not activate its window")
		}
		if err = wait(func() bool { return g.visible.Load() }); err != nil {
			return err
		}
	}
	for page, name := range []string{"overview", "devices", "audio", "input", "layout", "settings"} {
		fyne.DoAndWait(func() {
			g.status = g.service.Snapshot()
			g.config = g.service.Config()
			g.showPage(page)
		})
		// Native Capture reads the last GL frame. Allow the render loop to
		// present this page before capturing, rather than saving the prior page.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
		var frame image.Image
		frameContext, stopFrame := context.WithTimeout(ctx, 5*time.Second)
		for {
			fyne.DoAndWait(func() { frame = g.window.Canvas().Capture() })
			if visibleNativeFrame(frame) {
				break
			}
			select {
			case <-frameContext.Done():
				stopFrame()
				return fmt.Errorf("%s: native renderer returned blank frames (check the OpenGL/software renderer)", name)
			case <-time.After(100 * time.Millisecond):
			}
		}
		stopFrame()
		file, err := os.Create(filepath.Join(output, name+".png"))
		if err != nil {
			return err
		}
		err = png.Encode(file, frame)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if watcher != nil {
		if err := watcher.Wait(ctx); err != nil {
			return err
		}
	}
	if checkClipboard {
		sample := "SuperFlow 原生剪贴板验证"
		bridge := g.clipboardBackend()
		if err := bridge.Write(ctx, sample); err != nil {
			return err
		}
		value, err := bridge.Read(ctx)
		if err != nil || value != sample {
			return fmt.Errorf("native clipboard mismatch: %w", err)
		}
	}
	fyne.DoAndWait(g.closeWindow)
	if g.visible.Load() || g.service.Snapshot().State != "listening" {
		return errors.New("closing the window interrupted sharing")
	}
	if watcher != nil {
		if err := watcher.Activate(ctx); err != nil {
			return err
		}
		if err := wait(func() bool { return g.visible.Load() }); err != nil {
			return err
		}
		fyne.DoAndWait(g.closeWindow)
	}
	secondary, err := instance.Open(ctx, path)
	if err != nil {
		return err
	}
	if secondary != nil {
		secondary.Close()
		return errors.New("a second launch created another instance")
	}
	if err = wait(func() bool { return g.visible.Load() }); err != nil {
		return err
	}
	if watcher != nil {
		if err = watcher.Restart(ctx); err != nil {
			return err
		}
	}
	var actions []string
	fyne.DoAndWait(func() {
		g.showPage(0)
		if g.tray != nil {
			for _, item := range g.trayMenu.Items {
				if item.Action != nil && !item.IsSeparator {
					actions = append(actions, item.Label)
				}
			}
		}
	})
	if watcher != nil && !strings.Contains(strings.Join(actions, "|"), tr("Quit SuperFlow")) {
		return errors.New("native tray menu is incomplete")
	}
	report := struct {
		OS, Architecture, Backend                        string
		Tray, Clipboard, BackgroundSharing, SecondLaunch bool
		Actions                                          []string
	}{runtime.GOOS, runtime.GOARCH, "native Fyne/OpenGL", watcher != nil, checkClipboard, true, true, actions}
	data, _ := json.MarshalIndent(report, "", "  ")
	return os.WriteFile(filepath.Join(output, "report.json"), data, 0644)
}

// A successful GL context can still return black frames under CPU emulation.
// Require both the light app surfaces and dark text before accepting a page.
func visibleNativeFrame(frame image.Image) bool {
	if frame == nil || frame.Bounds().Dx() < 400 || frame.Bounds().Dy() < 300 {
		return false
	}
	var light, dark, samples int
	bounds := frame.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 16 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 16 {
			r, g, b, a := frame.At(x, y).RGBA()
			samples++
			if a > 0x8000 && r > 0x8000 && g > 0x8000 && b > 0x8000 {
				light++
			}
			if a > 0x8000 && r < 0x6400 && g < 0x6400 && b < 0x6400 {
				dark++
			}
		}
	}
	return light > samples/4 && dark > 5
}
