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
		selected = "已配置设备（当前未连接）"
		options = append(options, selected)
		ids[selected] = current
	}
	selectDevice := widget.NewSelect(options, nil)
	selectDevice.SetSelected(selected)
	return selectDevice, ids
}
func (g *gui) route(label, description string, args []string, capture, mic bool) (*routeEditor, fyne.CanvasObject) {
	kind := "playback"
	placeholder := "系统默认输出"
	if capture && mic {
		kind = "capture"
		placeholder = "系统默认麦克风"
	} else if capture {
		if runtime.GOOS == "darwin" {
			kind = "capture"
			placeholder = "请选择虚拟输入设备"
		} else {
			placeholder = "默认系统输出（回环采集）"
		}
	} else if mic {
		placeholder = "请选择虚拟输出设备"
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
		return nil, errors.New("请为远端麦克风选择虚拟音频输出设备")
	}
	if r.capture && !r.mic && runtime.GOOS == "darwin" && id == "" {
		return nil, errors.New("请为系统声音选择虚拟音频输入设备")
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
	header := g.heading("声音与麦克风", "配置发送与接收设备", nil)
	if g.config == nil {
		return container.NewVBox(header, space(24), card(widget.NewLabel("请先创建工作空间，再配置声音路由。")), widget.NewButton("创建工作空间", g.setup))
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
		return container.NewVBox(header, space(24), widget.NewProgressBarInfinite(), widget.NewLabel("正在读取音频设备…"))
	}
	c := g.config.Clone()
	sendSystem, sys := g.route("系统声音", "将本机系统声音发送给已连接设备。", c.Audio.SendSystem, true, false)
	sendMic, mic := g.route("本机麦克风", "仅在开启并保存后采集，目标离线时停止。", c.Audio.SendMicrophone, true, true)
	playSystem, play := g.route("远端系统声音", "将远端声音汇集到选择的输出设备。", c.Audio.PlaySystem, false, false)
	playMic, virtual := g.route("远端麦克风", "写入已有虚拟设备，供会议或录音软件使用。", c.Audio.PlayMicrophone, false, true)
	targets := []string{"全部在线设备（系统声音）"}
	if c.Mode == "node" {
		targets = []string{"使用键鼠的电脑"}
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
	target := widget.NewSelect(targets, nil)
	target.SetSelected(selected)
	left := card(container.NewVBox(text("发送", 19, textColor, true), space(14), sys, mic, widget.NewLabel("发送目标"), target))
	right := card(container.NewVBox(text("接收", 19, textColor, true), space(14), play, virtual))
	codec := widget.NewSelect([]string{"自动 · 推荐", "Opus · 高效传输", "PCM · 无压缩"}, nil)
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
			return errors.New("请输入 10–200 ms")
		}
		return nil
	}
	prefs := widget.NewAccordion(widget.NewAccordionItem("高级音频设置", container.NewVBox(explain("自动模式会根据双方能力选择编码，通常无需调整。"), container.NewGridWithColumns(2, container.NewVBox(text("音频编码", 12, mutedColor, false), codec), container.NewVBox(text("接收缓冲 / ms", 12, mutedColor, false), buffer)))))
	save := widget.NewButtonWithIcon("保存设置", theme.ConfirmIcon(), func() {
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
	refresh := widget.NewButtonWithIcon("刷新设备", theme.ViewRefreshIcon(), func() { g.audioLoaded = false; g.audioError = ""; g.showPage(2) })
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
	shortcuts := container.NewVBox(text("快捷配置", 14, textColor, true), container.NewGridWithColumns(3, quick("声音汇集到本机", "receive"), quick("共享本机系统声音", "send"), quick("关闭音频共享", "off")), explain("在其他电脑选择“共享本机系统声音”，在收听的电脑选择“声音汇集到本机”。麦克风单独开启。"))
	body := container.NewVBox(shortcuts, space(12), container.NewGridWithColumns(2, left, right), space(12), prefs, space(12))
	if g.audioError != "" {
		warning := widget.NewLabel(g.audioError)
		warning.Wrapping = fyne.TextWrapWord
		warning.Importance = widget.WarningImportance
		body.Add(warning)
	}
	body.Add(widget.NewLabel("保存后会重新建立共享连接；此操作不会自动安装虚拟音频驱动。"))
	return container.NewBorder(container.NewVBox(header, space(20)), container.NewVBox(space(12), footer), nil, nil, container.NewVScroll(body))
}
