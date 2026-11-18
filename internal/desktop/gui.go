//go:build desktop

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fynedesktop "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	core "superflow.local/superflow/internal/app"
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/brand"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/control"
	"superflow.local/superflow/internal/i18n"
	"superflow.local/superflow/internal/input"
	"superflow.local/superflow/internal/instance"
)

func Available() bool { return true }

type gui struct {
	trayMenu                  *fyne.Menu
	supportStatus             *widget.Label
	audioDevices              []audio.Device
	audioLoading, audioLoaded bool
	audioError                string
	deviceRows                map[string]*deviceRow
	app                       fyne.App
	window                    fyne.Window
	service                   *control.Service
	ctx                       context.Context
	cancel                    context.CancelFunc
	config                    *config.Config
	status                    core.Status
	content                   *fyne.Container
	nav                       []*widget.Button
	page                      int
	busy, quitting            bool
	start                     *widget.Button
	statusLabel               *widget.Label
	online, rtt, traffic      *canvas.Text
	workspace                 *fyne.Container
	devices                   *fyne.Container
	message                   *widget.Label
	tray                      fynedesktop.App
	lastTray                  string
	lastBytes                 map[string][2]uint64
	lastPoll                  time.Time
	up, down                  float64
	log                       *guiLog
	visible                   atomic.Bool
	workspaceKey              string
	discoveryRefresh          func()
	canRecover                bool
	trayAvailable             bool
	backgroundHint            *widget.Label
}
type guiLog struct {
	mu   sync.Mutex
	last string
}

func (l *guiLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := strings.TrimSpace(string(b))
	if len(s) > 2048 {
		s = s[:2048]
	}
	l.last = s
	return len(b), nil
}
func Run(ctx context.Context, path string, options ...Options) error {
	setLanguage(i18n.System)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	guard, err := instance.Open(ctx, path)
	if err != nil {
		return err
	}
	if guard == nil {
		return nil
	}
	defer guard.Close()
	service, err := control.New(ctx, path)
	if err != nil {
		return err
	}
	prepareNativeProcess()
	a := app.NewWithID(brand.ApplicationID)
	a.Settings().SetTheme(superTheme{})
	a.SetIcon(fyne.NewStaticResource("superflow.svg", []byte(brand.SVG)))
	g := newGUI(ctx, cancel, a, service)
	g.canRecover = true
	background := len(options) > 0 && options[0].Background && g.config != nil
	a.Lifecycle().SetOnStarted(func() {
		installReopen(g.reveal)
		if g.config == nil {
			fyne.Do(g.setup)
		}
	})
	a.Lifecycle().SetOnEnteredForeground(func() { fyne.Do(g.activated) })
	if err := service.EnableDiscovery(); err != nil {
		slog.Warn(tr("Network discovery is unavailable. Connection invitations still work."), "error", err)
	}
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(g.log, nil)))
	defer slog.SetDefault(original)
	defer func() {
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = service.Stop(stop)
	}()
	g.initializeTray()
	g.window.SetCloseIntercept(g.closeWindow)
	if background {
		g.visible.Store(false)
	} else {
		g.window.Show()
	}
	if g.config != nil && a.Preferences().BoolWithFallback("start-sharing", false) {
		g.perform("Resuming sharing…", g.service.Start, nil)
	}
	go g.watch()
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
	a.Run()
	return nil
}
func newGUI(ctx context.Context, cancel context.CancelFunc, a fyne.App, service *control.Service) *gui {
	setLanguage(a.Preferences().StringWithFallback(languagePreference, i18n.System))
	g := &gui{app: a, window: a.NewWindow("SuperFlow"), service: service, ctx: ctx, cancel: cancel, config: service.Config(), status: service.Snapshot(), lastBytes: map[string][2]uint64{}, log: &guiLog{}}
	g.visible.Store(true)
	_ = service.UseClipboard(g.clipboardBackend())
	g.window.Resize(fyne.NewSize(1120, 820))
	g.window.SetPadded(false)
	g.buildContent(0)
	g.installShortcuts()
	return g
}

func (g *gui) buildContent(page int) {
	g.nav = nil
	g.content = container.NewStack()
	g.message = widget.NewLabel(tr("Ready"))
	if g.busy {
		g.message.SetText(tr("Working…"))
	}
	g.message.Wrapping = fyne.TextWrapWord
	g.message.Importance = widget.LowImportance
	main := container.NewBorder(nil, container.NewVBox(rule(), g.message), nil, nil, g.content)
	root := container.NewBorder(nil, nil, g.sidebar(), nil, main)
	g.window.SetContent(root)
	g.showPage(page)
}
func (g *gui) sidebar() fyne.CanvasObject {
	logo := canvas.NewImageFromResource(fyne.NewStaticResource("superflow.svg", []byte(brand.SVG)))
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(28, 28))
	brandLine := container.NewHBox(logo, text("SuperFlow", 19, textColor, true))
	nav := container.NewVBox(space(12), brandLine, space(32), text(tr("Workspace"), 11, mutedColor, false), space(4))
	for i, item := range []struct {
		name string
		icon fyne.Resource
	}{{tr("Overview"), theme.HomeIcon()}, {tr("Devices"), theme.ComputerIcon()}, {tr("Audio & microphone"), theme.VolumeUpIcon()}, {tr("Input & clipboard"), theme.ContentCopyIcon()}, {tr("Screen layout"), theme.GridIcon()}, {tr("Settings"), theme.SettingsIcon()}} {
		index := i
		if i == 2 {
			nav.Add(space(20))
			nav.Add(text(tr("Sharing"), 11, mutedColor, false))
			nav.Add(space(4))
		}
		if i == 5 {
			nav.Add(space(20))
			nav.Add(rule())
			nav.Add(space(4))
		}
		b := widget.NewButtonWithIcon(item.name, item.icon, func() { g.showPage(index) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		g.nav = append(g.nav, b)
		nav.Add(b)
	}
	footer := container.NewVBox(rule(), space(8), text("SuperFlow  "+strings.Split(core.Release, "-")[0], 11, mutedColor, false), text(tr("Preview"), 10, mutedColor, false))
	inside := container.NewBorder(nav, footer, nil, nil, layout.NewSpacer())
	r := canvas.NewRectangle(sidebarColor)
	r.SetMinSize(fyne.NewSize(208, 1))
	return container.NewStack(r, inset(inside, 20))
}
func (g *gui) showPage(page int) {
	g.page = page
	for i, b := range g.nav {
		b.Importance = widget.LowImportance
		if i == page {
			b.Importance = widget.MediumImportance
		}
		b.Refresh()
	}
	g.start = nil
	g.statusLabel = nil
	g.online = nil
	g.rtt = nil
	g.traffic = nil
	g.workspace = nil
	g.workspaceKey = ""
	g.backgroundHint = nil
	g.supportStatus = nil
	g.devices = nil
	var view fyne.CanvasObject
	switch page {
	case 0:
		view = g.overviewPage()
	case 1:
		view = g.devicesPage()
	case 2:
		view = g.audioPage()
	case 3:
		view = g.inputPage()
	case 4:
		view = g.layoutPage()
	default:
		view = g.settingsPage()
	}
	g.content.Objects = []fyne.CanvasObject{inset(view, 32)}
	g.content.Refresh()
	g.refresh()
}
func (g *gui) heading(title, subtitle string, action fyne.CanvasObject) fyne.CanvasObject {
	left := container.NewVBox(text(title, 24, textColor, true), space(2), text(subtitle, 12, mutedColor, false))
	if action != nil {
		action = container.NewCenter(action)
	}
	return container.NewBorder(nil, nil, left, action, layout.NewSpacer())
}
func (g *gui) overviewPage() fyne.CanvasObject {
	g.statusLabel = widget.NewLabel("")
	g.statusLabel.Importance = widget.LowImportance
	g.start = widget.NewButtonWithIcon(tr("Start sharing"), theme.MediaPlayIcon(), g.toggle)
	g.start.Importance = widget.HighImportance
	add := widget.NewButtonWithIcon(tr("Add device"), theme.ContentAddIcon(), g.addDevice)
	header := g.heading(tr("Overview"), tr("Manage connections and sharing"), container.NewHBox(add, g.start))
	g.online = text(fmt.Sprintf(tr("%d"), 0), 13, textColor, false)
	g.rtt = text("—", 13, textColor, false)
	g.traffic = text("↑ 0 K  ↓ 0 K", 13, textColor, false)
	metric := func(label string, value fyne.CanvasObject, width float32) fyne.CanvasObject {
		min := canvas.NewRectangle(background)
		min.SetMinSize(fyne.NewSize(width, 1))
		return container.NewStack(min, container.NewHBox(text(label, 12, mutedColor, false), value))
	}
	metrics := container.NewHBox(metric(tr("Online"), g.online, 84), metric(tr("RTT"), g.rtt, 112), metric(tr("Transfer"), g.traffic, 168))
	telemetry := container.NewBorder(nil, nil, g.statusLabel, metrics, nil)
	edit := widget.NewButtonWithIcon(tr("Arrange screens"), theme.SettingsIcon(), func() { g.showPage(4) })
	edit.Importance = widget.LowImportance
	mapHeading := container.NewBorder(nil, nil, text(tr("Device layout"), 14, textColor, true), edit, nil)
	g.workspace = container.NewStack(space(190))
	board := container.NewBorder(container.NewVBox(mapHeading, space(8)), container.NewVBox(space(8), rule(), telemetry), nil, nil, g.workspace)
	section := container.NewVBox(text(tr("Shared features"), 14, textColor, true), space(8), g.overviewRoutes())
	footer := container.NewHBox(text(tr("Shortcuts"), 11, mutedColor, false), text("Ctrl + Alt + Esc", 11, textColor, false), text(tr("Return to this computer"), 11, mutedColor, false), layout.NewSpacer(), text(tr("Closes to background · Reopen to return"), 11, mutedColor, false))
	bottom := container.NewVBox(space(16), section, space(12), footer)
	return container.NewBorder(container.NewVBox(header, space(20), rule(), space(12)), bottom, nil, nil, board)
}
func (g *gui) watch() {
	clock := time.NewTimer(time.Second)
	defer clock.Stop()
	var nextTrayProbe time.Time
	for {
		select {
		case <-g.ctx.Done():
			fyne.Do(func() {
				if !g.quitting {
					g.quitting = true
					g.app.Quit()
				}
			})
			return
		case <-clock.C:
			probe := time.Now().After(nextTrayProbe)
			var trayAvailable bool
			if probe {
				trayAvailable = hasTray()
				nextTrayProbe = time.Now().Add(5 * time.Second)
			}
			snapshot := g.service.Snapshot()
			visible := g.visible.Load()
			var cfg *config.Config
			if visible {
				cfg = g.service.Config()
			}
			fyne.Do(func() {
				if probe {
					g.syncTray(trayAvailable)
				}
				g.status = snapshot
				if g.visible.Load() && visible {
					g.config = cfg
					g.refresh()
					if g.discoveryRefresh != nil {
						g.discoveryRefresh()
					}
				} else {
					g.updateTray()
				}
			})
			delay := time.Second
			if !visible {
				delay = 5 * time.Second
			}
			clock.Reset(delay)
		}
	}
}
func (g *gui) refresh() {
	now := time.Now()
	elapsed := now.Sub(g.lastPoll).Seconds()
	g.up = 0
	g.down = 0
	next := map[string][2]uint64{}
	for _, p := range g.status.Peers {
		if old, ok := g.lastBytes[p.ID]; ok && elapsed > 0 {
			if p.BytesSent >= old[0] {
				g.up += float64(p.BytesSent-old[0]) / elapsed
			}
			if p.BytesReceived >= old[1] {
				g.down += float64(p.BytesReceived-old[1]) / elapsed
			}
		}
		next[p.ID] = [2]uint64{p.BytesSent, p.BytesReceived}
	}
	g.lastPoll = now
	g.lastBytes = next
	if g.start != nil {
		if g.config == nil {
			g.start.SetText(tr("Set up workspace"))
			g.start.SetIcon(theme.ContentAddIcon())
		} else if running(g.status.State) {
			g.start.SetText(tr("Pause sharing"))
			g.start.SetIcon(theme.MediaPauseIcon())
		} else {
			g.start.SetText(tr("Start sharing"))
			g.start.SetIcon(theme.MediaPlayIcon())
		}
		if g.busy {
			g.start.Disable()
		} else {
			g.start.Enable()
		}
	}
	if g.statusLabel != nil {
		g.statusLabel.SetText(stateText(g.status.State))
	}
	if g.online != nil {
		g.online.Text = fmt.Sprintf(tr("%d"), len(g.status.Peers))
		g.online.Refresh()
	}
	if g.rtt != nil {
		g.rtt.Text = "—"
		if len(g.status.Peers) > 0 {
			var rtt float64
			for _, p := range g.status.Peers {
				rtt += p.RTTMS
			}
			g.rtt.Text = fmt.Sprintf("%.1f ms", rtt/float64(len(g.status.Peers)))
		}
		g.rtt.Refresh()
	}
	if g.traffic != nil {
		g.traffic.Text = fmt.Sprintf("↑ %s  ↓ %s", bytesRate(g.up), bytesRate(g.down))
		g.traffic.Refresh()
	}
	if g.workspace != nil {
		key := g.workspaceSignature()
		if key != g.workspaceKey {
			g.workspaceKey = key
			g.workspace.Objects = []fyne.CanvasObject{g.workspaceView()}
			g.workspace.Refresh()
		}
	}
	if g.devices != nil {
		g.fillDevices()
	}
	if g.status.LastError != "" && !g.busy {
		g.message.SetText(g.status.LastError)
	}
	g.updateTray()
}
func (g *gui) perform(success string, fn func() error, after func()) {
	if g.busy {
		return
	}
	g.busy = true
	g.message.SetText(tr("Working…"))
	g.refresh()
	go func() {
		err := fn()
		fyne.Do(func() {
			g.busy = false
			g.status = g.service.Snapshot()
			g.config = g.service.Config()
			if err != nil {
				g.message.SetText(err.Error())
				g.showError(err)
			} else {
				g.message.SetText(tr(success))
				if after != nil {
					after()
				}
			}
			g.refresh()
		})
	}()
}
func (g *gui) toggle() {
	if g.config == nil {
		g.setup()
		return
	}
	if running(g.status.State) {
		g.perform("Sharing paused", func() error {
			ctx, cancel := context.WithTimeout(g.ctx, 5*time.Second)
			defer cancel()
			return g.service.Stop(ctx)
		}, nil)
	} else {
		if !g.checkInput(g.config) {
			return
		}
		g.perform("Starting connection…", g.service.Start, nil)
	}
}
func (g *gui) applyFrom(expected, next *config.Config) {
	if next.Input && !expected.Input {
		if len(next.InputDevices) == 0 {
			next.InputDevices = input.SuggestedDevices()
		}
		if !g.checkInput(next) {
			return
		}
	}
	g.perform("Settings saved", func() error {
		ctx, cancel := context.WithTimeout(g.ctx, 5*time.Second)
		defer cancel()
		return g.service.Apply(ctx, expected, next)
	}, func() { g.showPage(g.page) })
}
func (g *gui) reveal() {
	g.activated()
	g.window.Show()
	g.window.RequestFocus()
	revealNative(g.window)
}
func (g *gui) activated() {
	g.visible.Store(true)
	g.config = g.service.Config()
	g.status = g.service.Snapshot()
	g.refresh()
}

func (g *gui) workspaceSignature() string {
	var screens []config.Screen
	name, mode := tr("This computer"), ""
	if g.config != nil {
		screens = g.config.Screens
		name = g.config.Name
		mode = g.config.Mode
	}
	var peers []string
	for _, p := range g.status.Peers {
		peers = append(peers, p.ID, p.Name)
	}
	b, _ := json.Marshal(struct {
		Name, Mode, Target, State string
		Screens                   []config.Screen
		Peers                     []string
	}{name, mode, g.status.Target, g.status.State, screens, peers})
	return string(b)
}
func (g *gui) quit() {
	if g.quitting {
		return
	}
	g.quitting = true
	g.cancel()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = g.service.Stop(ctx)
		fyne.Do(func() { g.app.Quit() })
	}()
}
func (g *gui) updateTray() {
	if g.tray == nil {
		return
	}
	key := g.status.State + fmt.Sprint(len(g.status.Peers))
	if key == g.lastTray {
		return
	}
	g.lastTray = key
	toggle := tr("Start sharing")
	if running(g.status.State) {
		toggle = tr("Pause sharing")
	}
	status := fyne.NewMenuItem("SuperFlow · "+stateText(g.status.State), nil)
	status.Disabled = true
	item := func(label string, action func()) *fyne.MenuItem {
		return fyne.NewMenuItem(label, func() { fyne.Do(action) })
	}
	quit := item(tr("Quit SuperFlow"), g.quit)
	quit.IsQuit = true
	menu := fyne.NewMenu("SuperFlow", status, fyne.NewMenuItemSeparator(), item(tr("Open SuperFlow"), g.reveal), item(toggle, g.toggle), item(tr("Return to this computer"), func() { g.perform("Returned to this computer", g.service.ReturnLocal, nil) }), item(tr("Add device"), func() { g.reveal(); g.addDevice() }), fyne.NewMenuItemSeparator(), quit)
	g.tray.SetSystemTrayMenu(menu)
	g.trayMenu = menu
}
func running(state string) bool {
	return state == "starting" || state == "connecting" || state == "connected" || state == "listening" || state == "stopping"
}
func stateText(state string) string {
	if text, ok := map[string]string{"unconfigured": tr("Not configured"), "stopped": tr("Sharing stopped"), "starting": tr("Starting…"), "connecting": tr("Connecting…"), "connected": tr("Device connected"), "listening": tr("Waiting for devices"), "stopping": tr("Stopping…"), "error": tr("Check settings")}[state]; ok {
		return text
	}
	return state
}
func bytesRate(value float64) string {
	if value >= 1024*1024 {
		return fmt.Sprintf("%.1f M", value/1024/1024)
	}
	return fmt.Sprintf("%.0f K", value/1024)
}
func gainValue(c *config.Config, name string) float64 {
	if c != nil {
		if gain, ok := c.Audio.Gains[name]; ok {
			return gain
		}
	}
	return 1
}
