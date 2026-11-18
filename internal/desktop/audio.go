//go:build desktop

package desktop

import (
	"errors"
	"runtime"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"superflow.local/superflow/internal/assist"
	"superflow.local/superflow/internal/audio"
)

type routeEditor struct {
	on           *toggle
	device       *widget.Select
	ids          map[string]string
	original     []string
	capture, mic bool
}

func argumentDevice(args []string) string {
	for i, a := range args {
		if a == "--device" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
func (g *gui) deviceChoice(kind, current, placeholder string) (*widget.Select, map[string]string) {
	options := []string{placeholder}
	ids := map[string]string{placeholder: ""}
	selected := placeholder
	for _, device := range g.audioDevices {
		if device.Kind != kind {
			continue
		}
		name := device.Name
		if _, exists := ids[name]; exists {
			name += " · " + device.ID
		}
		ids[name] = device.ID
		options = append(options, name)
		if device.ID == current {
			selected = name
		}
	}
	if current != "" && selected == placeholder {
		selected = tr("Configured device (disconnected)")
		options = append(options, selected)
		ids[selected] = current
	}
	selectDevice := newSelect(options, nil)
	selectDevice.SetSelected(selected)
	return selectDevice, ids
}
func (g *gui) route(label, description string, args []string, capture, mic bool) (*routeEditor, fyne.CanvasObject) {
	kind := "playback"
	placeholder := tr("Default system output")
	if capture && mic {
		kind = "capture"
		placeholder = tr("Default microphone")
	} else if capture {
		if runtime.GOOS == "darwin" {
			kind = "capture"
			placeholder = tr("Choose a virtual input")
		} else {
			placeholder = tr("Default output (loopback)")
		}
	} else if mic {
		placeholder = tr("Choose a virtual output")
	}
	current := argumentDevice(args)
	if len(args) == 0 && (mic && !capture || capture && !mic && runtime.GOOS == "darwin") {
		current = assist.UniqueVirtual(g.audioDevices, kind)
	}
	selectDevice, ids := g.deviceChoice(kind, current, placeholder)
	editor := &routeEditor{on: newToggle(len(args) > 0), device: selectDevice, ids: ids, original: append([]string(nil), args...), capture: capture, mic: mic}
	header := container.NewBorder(nil, nil, text(label, 14, textColor, true), editor.on, nil)
	hint := widget.NewLabel(description)
	hint.Importance = widget.LowImportance
	hint.Wrapping = fyne.TextWrapWord
	if !editor.on.Checked {
		selectDevice.Hide()
	}
	editor.on.OnChanged = func(on bool) {
		if on {
			selectDevice.Show()
		} else {
			selectDevice.Hide()
		}
	}
	return editor, container.NewVBox(header, hint, selectDevice, space(16))
}
func (r *routeEditor) command() ([]string, error) {
	on := r.on.Checked
	id := r.ids[r.device.Selected]
	if on == (len(r.original) > 0) && id == argumentDevice(r.original) {
		return append([]string(nil), r.original...), nil
	}
	if !on {
		return nil, nil
	}
	if r.mic && !r.capture && id == "" {
		return nil, errors.New(tr("Choose a virtual audio output for the remote microphone"))
	}
	if r.capture && !r.mic && runtime.GOOS == "darwin" && id == "" {
		return nil, errors.New(tr("Choose a virtual audio input for system audio"))
	}
	args := []string{"@self", "audio-play"}
	if r.capture {
		args = []string{"@self", "audio-capture"}
		if !r.mic && runtime.GOOS != "darwin" {
			args = append(args, "--loopback")
		}
	}
	if id != "" {
		args = append(args, "--device", id)
	}
	return args, nil
}
func (g *gui) audioPage() fyne.CanvasObject {
	header := g.heading(tr("Audio & microphone"), tr("Choose devices for sending and receiving audio"), nil)
	if g.config == nil {
		return container.NewVBox(header, space(24), card(widget.NewLabel(tr("Create a workspace to set up audio routes."))), widget.NewButton(tr("Create workspace"), g.setup))
	}
	if !g.audioLoaded {
		if !g.audioLoading {
			g.audioLoading = true
			go func() {
				devices, err := audio.Devices()
				fyne.Do(func() {
					g.audioDevices = devices
					g.audioLoaded = true
					g.audioLoading = false
					if err != nil {
						g.audioError = err.Error()
					}
					if g.page == 2 {
						g.showPage(2)
					}
				})
			}()
		}
		return container.NewVBox(header, space(24), widget.NewProgressBarInfinite(), widget.NewLabel(tr("Reading audio devices…")))
	}
	c := g.config.Clone()
	sendSystem, sys := g.route(tr("System audio"), tr("Send this computer's system audio to connected devices."), c.Audio.SendSystem, true, false)
	sendMic, mic := g.route(tr("Local microphone"), tr("Starts after you enable and save it, and stops when the destination goes offline."), c.Audio.SendMicrophone, true, true)
	playSystem, play := g.route(tr("Remote system audio"), tr("Play audio from connected computers on the selected output."), c.Audio.PlaySystem, false, false)
	playMic, virtual := g.route(tr("Remote microphone"), tr("Send to an existing virtual audio device for calls or recording."), c.Audio.PlayMicrophone, false, true)
	targets := []string{tr("All online devices (system audio)")}
	if c.Mode == "node" {
		targets = []string{tr("Keyboard and mouse host")}
	}
	for _, name := range g.peerNames() {
		targets = append(targets, name)
	}
	selected := targets[0]
	if c.Audio.Target != "" {
		selected = c.Audio.Target
		found := false
		for _, name := range targets {
			if name == selected {
				found = true
			}
		}
		if !found {
			targets = append(targets, selected)
		}
	}
	target := newSelect(targets, nil)
	target.SetSelected(selected)
	left := card(container.NewVBox(text(tr("Send"), 19, textColor, true), space(14), sys, mic, widget.NewLabel(tr("Send to")), target))
	right := card(container.NewVBox(text(tr("Receive"), 19, textColor, true), space(14), play, virtual))
	codec := newSelect([]string{tr("Auto · Recommended"), tr("Opus · Efficient"), tr("PCM · Uncompressed")}, nil)
	codec.SetSelectedIndex(0)
	if c.Audio.Codec == "opus" {
		codec.SetSelectedIndex(1)
	}
	if c.Audio.Codec == "pcm" {
		codec.SetSelectedIndex(2)
	}
	buffer := widget.NewEntry()
	buffer.SetText(strconv.Itoa(c.Audio.BufferMS))
	buffer.Validator = func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil || n < 10 || n > 200 {
			return errors.New(tr("Enter a value from 10 to 200 ms"))
		}
		return nil
	}
	prefs := widget.NewAccordion(widget.NewAccordionItem(tr("Advanced audio settings"), container.NewVBox(explain(tr("Auto selects a codec both computers support.")), container.NewGridWithColumns(2, container.NewVBox(text(tr("Codec"), 12, mutedColor, false), codec), container.NewVBox(text(tr("Receive buffer (ms)"), 12, mutedColor, false), buffer)))))
	save := widget.NewButtonWithIcon(tr("Save settings"), theme.ConfirmIcon(), func() {
		if err := buffer.Validate(); err != nil {
			g.message.SetText(err.Error())
			return
		}
		next := c.Clone()
		var err error
		for _, pair := range []struct {
			route *routeEditor
			dst   *[]string
		}{{sendSystem, &next.Audio.SendSystem}, {sendMic, &next.Audio.SendMicrophone}, {playSystem, &next.Audio.PlaySystem}, {playMic, &next.Audio.PlayMicrophone}} {
			*pair.dst, err = pair.route.command()
			if err != nil {
				g.showError(err)
				return
			}
		}
		next.Audio.BufferMS, _ = strconv.Atoi(buffer.Text)
		next.Audio.Codec = "auto"
		if codec.SelectedIndex() == 1 {
			next.Audio.Codec = "opus"
		}
		if codec.SelectedIndex() == 2 {
			next.Audio.Codec = "pcm"
		}
		next.Audio.Target = target.Selected
		if target.Selected == targets[0] {
			next.Audio.Target = ""
		}
		g.applyFrom(c, next)
	})
	save.Importance = widget.HighImportance
	refresh := widget.NewButtonWithIcon(tr("Refresh devices"), theme.ViewRefreshIcon(), func() { g.audioLoaded = false; g.audioError = ""; g.showPage(2) })
	footer := container.NewHBox(layout.NewSpacer(), refresh, save)
	quick := func(label, profile string) *widget.Button {
		return widget.NewButton(label, func() {
			next, err := assist.AudioPreset(c, profile, runtime.GOOS, g.audioDevices)
			if err != nil {
				g.showError(err)
				return
			}
			g.applyFrom(c, next)
		})
	}
	shortcuts := container.NewVBox(text(tr("Audio presets"), 14, textColor, true), container.NewGridWithColumns(3, quick(tr("Listen on this computer"), "receive"), quick(tr("Share system audio"), "send"), quick(tr("Turn off audio sharing"), "off")), explain(tr("Select “Share system audio” on the source and “Listen on this computer” where you want to hear it. Enable microphone sharing separately.")))
	body := container.NewVBox(shortcuts, space(12), container.NewGridWithColumns(2, left, right), space(12), prefs, space(12))
	if g.audioError != "" {
		warning := widget.NewLabel(g.audioError)
		warning.Wrapping = fyne.TextWrapWord
		warning.Importance = widget.WarningImportance
		body.Add(warning)
	}
	body.Add(widget.NewLabel(tr("Saving reconnects sharing. Set up any required virtual audio devices in your operating system.")))
	return container.NewBorder(container.NewVBox(header, space(20)), container.NewVBox(space(12), footer), nil, nil, container.NewVScroll(body))
}
