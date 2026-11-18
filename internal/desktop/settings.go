//go:build desktop

package desktop

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	core "superflow.local/superflow/internal/app"
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/i18n"
	"superflow.local/superflow/internal/input"
	"superflow.local/superflow/internal/integration"
)

func (g *gui) settingsPage() fyne.CanvasObject {
	header := g.heading(tr("Settings"), tr("Preferences, system checks, and installation"), nil)
	language := card(container.NewBorder(nil, nil, widget.NewLabel(tr("Language")), g.languagePicker(nil), nil))
	background := newToggle(g.app.Preferences().BoolWithFallback("close-to-tray", true))
	background.OnChanged = func(value bool) { g.app.Preferences().SetBool("close-to-tray", value) }
	auto := newToggle(g.app.Preferences().BoolWithFallback("start-sharing", false))
	auto.OnChanged = func(value bool) { g.app.Preferences().SetBool("start-sharing", value) }
	login := newToggle(integration.AutoStartEnabled())
	login.OnChanged = func(value bool) {
		if g.busy {
			login.Checked = integration.AutoStartEnabled()
			login.Refresh()
			return
		}
		g.perform("Sign-in preference saved. It takes effect at the next sign-in.", func() error {
			err := integration.SetAutoStart(value, g.service.Path())
			fyne.Do(func() { login.Checked = integration.AutoStartEnabled(); login.Refresh() })
			return err
		}, nil)
	}
	g.backgroundHint = explain(g.backgroundDescription())
	behavior := card(container.NewVBox(text(tr("App behavior"), 17, textColor, true), space(10), container.NewBorder(nil, nil, widget.NewLabel(tr("Keep running when the window closes")), background, nil), g.backgroundHint, space(4), container.NewBorder(nil, nil, widget.NewLabel(tr("Resume sharing when SuperFlow opens")), auto, nil), container.NewBorder(nil, nil, widget.NewLabel(tr("Start in the background at sign-in")), login, nil), explain(tr("Sign-in startup installs SuperFlow for the current user without requiring administrator access."))))
	status := explain(tr("Checking system…"))
	g.supportStatus = status
	refresh := widget.NewButtonWithIcon(tr("Check again"), theme.ViewRefreshIcon(), func() { g.checkSystem(status) })
	detect := widget.NewButton(tr("Detect input devices"), func() {
		if g.config == nil {
			g.setup()
			return
		}
		next := g.config.Clone()
		next.InputDevices = input.SuggestedDevices()
		width, height := input.DesktopSize()
		if width > 0 && height > 0 {
			next.ScreenWidth = width
			next.ScreenHeight = height
		}
		g.applyFrom(g.config.Clone(), next)
	})
	tools := container.NewHBox(refresh, detect, widget.NewButton(tr("Sound settings"), func() { g.openSoundSettings(false) }), widget.NewButton(tr("Microphone permissions"), func() { g.openSoundSettings(true) }))
	system := card(container.NewVBox(text(tr("System check"), 17, textColor, true), status, tools))
	install := widget.NewButtonWithIcon(tr("Install SuperFlow"), theme.DownloadIcon(), func() {
		var destination string
		g.perform("SuperFlow is now available in the app menu", func() error { var err error; destination, err = integration.Install(g.service.Path()); return err }, func() { g.message.SetText(tr("Installed: ") + destination) })
	})
	copy := widget.NewButtonWithIcon(tr("Copy diagnostics"), theme.ContentCopyIcon(), func() {
		g.window.Clipboard().SetContent(g.diagnosticSummary())
		g.message.SetText(tr("Copied diagnostics without pairing credentials or clipboard text"))
	})
	installCard := card(container.NewVBox(text(tr("Installation & support"), 17, textColor, true), explain(tr("Open SuperFlow from the app menu after installation. Your paired devices remain available.")), container.NewHBox(install, copy), widget.NewButton(tr("Advanced configuration"), g.advancedSettings)))
	about := container.NewHBox(text("SuperFlow", 14, textColor, true), text(core.Release, 11, mutedColor, false))
	g.checkSystem(status)
	return container.NewVScroll(container.NewVBox(header, space(20), language, space(12), behavior, space(12), system, space(12), installCard, space(12), about))
}
func (g *gui) checkSystem(label *widget.Label) {
	c := g.config.Clone()
	go func() {
		options := input.Options{}
		if c != nil {
			options = input.Options{Capture: c.Mode == "hub", Devices: c.InputDevices, Width: c.ScreenWidth, Height: c.ScreenHeight}
		} else {
			options.Devices = input.SuggestedDevices()
			options.Capture = true
		}
		keys := tr("Keyboard and mouse ready")
		if err := input.Check(options); err != nil {
			keys = tr("Input setup required: ") + err.Error()
		}
		codec := tr("Opus available")
		if audio.OpusAvailable() != nil {
			codec = tr("PCM fallback available")
		}
		message := keys + tr("\nNative clipboard ready · ") + codec
		fyne.Do(func() {
			if g.supportStatus == label {
				label.SetText(message)
			}
		})
	}()
}
func (g *gui) openSoundSettings(microphone bool) {
	switch runtime.GOOS {
	case "windows":
		if microphone {
			g.openURL("ms-settings:privacy-microphone")
		} else {
			g.openURL("ms-settings:sound")
		}
	case "darwin":
		if microphone {
			g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")
		} else {
			g.openURL("x-apple.systempreferences:com.apple.Sound-Settings.extension")
		}
	default:
		for _, candidate := range [][]string{{"gnome-control-center", "sound"}, {"systemsettings", "kcm_pulseaudio"}, {"pavucontrol"}} {
			if _, err := exec.LookPath(candidate[0]); err != nil {
				continue
			}
			command := exec.Command(candidate[0], candidate[1:]...)
			if err := command.Start(); err == nil {
				_ = command.Process.Release()
				return
			}
		}
		g.showError(i18n.Error("Choose your input and output devices in your desktop's sound settings"))
	}
}
func (g *gui) diagnosticSummary() string {
	sharing := tr("Stopped")
	if running(g.status.State) {
		sharing = tr("Running")
	}
	return strings.Join([]string{"SuperFlow " + core.Release, tr("System: ") + runtime.GOOS + "/" + runtime.GOARCH, tr("Sharing: ") + sharing, fmt.Sprintf(tr("Online devices: %d"), len(g.status.Peers)), fmt.Sprintf(tr("Tray: %t"), g.trayAvailable), tr("Clipboard: native desktop")}, "\n")
}
