//go:build desktop

package desktop

import (
	"fmt"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	core "superflow.local/superflow/internal/app"
	"superflow.local/superflow/internal/config"
)

type deviceTile struct {
	widget.BaseWidget
	name, detail string
	icon         fyne.Resource
	active       bool
	tap          func()
}

func newTile(name, detail string, icon fyne.Resource, active bool, tap func()) *deviceTile {
	t := &deviceTile{name: name, detail: detail, icon: icon, active: active, tap: tap}
	t.ExtendBaseWidget(t)
	return t
}
func (t *deviceTile) CreateRenderer() fyne.WidgetRenderer {
	return newMonitorRenderer(t)
}
func (t *deviceTile) Tapped(*fyne.PointEvent) {
	if t.tap != nil {
		t.tap()
	}
}
func (t *deviceTile) AccessibilityLabel() string             { return t.name + "，" + t.detail }
func (t *deviceTile) AccessibilityRole() fyne.AccessibleRole { return fyne.AccessibleRoleButton }
func (g *gui) peer(name string) *core.PeerStatus {
	for i := range g.status.Peers {
		if g.status.Peers[i].Name == name {
			return &g.status.Peers[i]
		}
	}
	return nil
}
func (g *gui) peerNames() []string {
	seen := map[string]bool{}
	if g.config != nil {
		for _, screen := range g.config.Screens {
			if screen.Name != g.config.Name {
				seen[screen.Name] = true
			}
		}
	}
	for _, peer := range g.status.Peers {
		seen[peer.Name] = true
	}
	var names []string
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func (g *gui) workspaceView() fyne.CanvasObject {
	name := "本机"
	screens := []config.Screen{{Name: name}}
	if g.config != nil {
		name = g.config.Name
		screens = append([]config.Screen(nil), g.config.Screens...)
	}
	details := map[string]string{name: "本机"}
	for _, p := range g.status.Peers {
		details[p.Name] = "已连接"
	}
	for _, s := range screens {
		if _, ok := details[s.Name]; !ok {
			details[s.Name] = "离线"
		}
	}
	maxX := 0
	for _, s := range screens {
		maxX = max(maxX, s.X)
	}
	screens = append(screens, config.Screen{Name: "\x00add", X: maxX + 1})
	board := newScreenBoard(screens, name, nil)
	board.readOnly = true
	board.focus = g.status.Target
	board.details = details
	board.onTap = func(target string) {
		if target == "\x00add" {
			g.addDevice()
			return
		}
		if target == name {
			if g.config == nil {
				g.setup()
			} else if g.config.Mode == "hub" {
				g.perform("已返回本机", g.service.ReturnLocal, nil)
			}
			return
		}
		if g.peer(target) != nil && g.config != nil && g.config.Mode == "hub" && g.config.Input {
			g.perform("键鼠目标已切换", func() error { return g.service.Select(target) }, nil)
		}
	}
	return container.NewScroll(board)
}

func (g *gui) overviewRoutes() fyne.CanvasObject {
	input, clipboard, sound := "未开启", "未开启", "尚未设置路由"
	if g.config != nil {
		if g.config.Input {
			input = "已开启"
		}
		if g.config.Clipboard {
			clipboard = "文本同步已开启"
		}
		a := g.config.Audio
		count := 0
		for _, route := range [][]string{a.SendSystem, a.SendMicrophone, a.PlaySystem, a.PlayMicrophone} {
			if len(route) > 0 {
				count++
			}
		}
		if count > 0 {
			sound = fmt.Sprintf("%d 条路由已配置", count)
		}
	}
	rows := container.New(layout.NewCustomPaddedVBoxLayout(0))
	for _, item := range []struct {
		title, state string
		icon         fyne.Resource
		page         int
	}{{"键盘与鼠标", input, theme.DesktopIcon(), 3}, {"声音与麦克风", sound, theme.VolumeUpIcon(), 2}, {"剪贴板", clipboard, theme.ContentCopyIcon(), 3}} {
		page := item.page
		button := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { g.showPage(page) })
		button.Importance = widget.LowImportance
		label := container.NewHBox(widget.NewIcon(item.icon), text(item.title, 13, textColor, false))
		right := container.NewHBox(text(item.state, 12, mutedColor, false), button)
		rows.Add(container.NewBorder(nil, nil, label, right, layout.NewSpacer()))
		rows.Add(rule())
	}
	return rows
}

type deviceRow struct {
	root            fyne.CanvasObject
	status, metrics *widget.Label
	control         *widget.Button
	volume          *widget.Slider
	gain            *widget.Label
}

func (g *gui) devicesPage() fyne.CanvasObject {
	add := widget.NewButtonWithIcon("添加设备", theme.ContentAddIcon(), g.addDevice)
	add.Importance = widget.HighImportance
	g.devices = container.NewVBox()
	g.deviceRows = map[string]*deviceRow{}
	return container.NewBorder(container.NewVBox(g.heading("设备", "连接状态与每台设备的系统音量", add), space(24)), nil, nil, nil, container.NewVScroll(g.devices))
}
func (g *gui) newDeviceRow(name string) *deviceRow {
	r := &deviceRow{status: widget.NewLabel("等待连接"), metrics: widget.NewLabel("—"), gain: widget.NewLabel("")}
	r.status.Importance = widget.LowImportance
	r.metrics.Importance = widget.LowImportance
	r.metrics.Wrapping = fyne.TextWrapWord
	r.control = widget.NewButtonWithIcon("控制设备", theme.DesktopIcon(), func() { g.perform("键鼠目标已切换", func() error { return g.service.Select(name) }, nil) })
	header := container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(theme.ComputerIcon()), text(name, 17, textColor, true)), r.control, nil)
	r.volume = widget.NewSlider(0, 2)
	r.volume.Step = .05
	r.volume.SetValue(gainValue(g.config, name))
	r.gain.SetText(fmt.Sprintf("%.0f%%", r.volume.Value*100))
	r.volume.OnChanged = func(gain float64) { r.gain.SetText(fmt.Sprintf("%.0f%%", gain*100)) }
	r.volume.OnChangeEnded = func(gain float64) {
		g.perform("设备音量已保存", func() error { return g.service.SetGain(name, gain) }, nil)
	}
	mute := widget.NewButtonWithIcon("", theme.VolumeMuteIcon(), func() {
		value := 0.0
		if r.volume.Value == 0 {
			value = 1
		}
		r.volume.SetValue(value)
		g.perform("设备音量已保存", func() error { return g.service.SetGain(name, value) }, nil)
	})
	volume := container.NewBorder(nil, nil, container.NewHBox(text("系统音量", 12, mutedColor, false), mute), r.gain, r.volume)
	r.root = card(container.NewVBox(header, r.status, r.metrics, space(8), volume))
	return r
}
func (g *gui) fillDevices() {
	names := g.peerNames()
	if len(names) == 0 {
		button := widget.NewButtonWithIcon("连接第一台设备", theme.ContentAddIcon(), g.addDevice)
		g.devices.Objects = []fyne.CanvasObject{card(container.NewVBox(text("你的设备会显示在这里", 21, textColor, true), widget.NewLabel("先添加设备名称，再在另一台电脑完成配对。"), space(8), button))}
		g.devices.Refresh()
		return
	}
	var objects []fyne.CanvasObject
	for _, name := range names {
		row := g.deviceRows[name]
		if row == nil {
			row = g.newDeviceRow(name)
			g.deviceRows[name] = row
		}
		peer := g.peer(name)
		if peer == nil {
			row.status.SetText("离线 · 连接后自动恢复")
			row.metrics.SetText("等待建立加密连接")
			row.control.Disable()
		} else {
			row.status.SetText("已连接  ·  " + peer.Address)
			row.metrics.SetText(fmt.Sprintf("往返 %.1f ms    系统缓冲 %d ms    音频补偿 %d 包    网络丢包 %d", peer.RTTMS, peer.Audio.System.BufferedMS, peer.Audio.System.Concealed, peer.PacketsLost))
			if g.config != nil && g.config.Input && !g.busy {
				row.control.Enable()
			} else {
				row.control.Disable()
			}
		}
		if g.busy {
			row.volume.Disable()
		} else {
			row.volume.Enable()
		}
		objects = append(objects, row.root, space(8))
	}
	g.devices.Objects = objects
	g.devices.Refresh()
}
func (g *gui) layoutPage() fyne.CanvasObject {
	edit := widget.NewButtonWithIcon("编辑布局", theme.SettingsIcon(), g.editLayout)
	edit.Importance = widget.HighImportance
	body := container.NewVBox(g.heading("屏幕布局", "按真实方位连接设备，鼠标可从相邻边缘切换。", edit), space(24))
	if g.config == nil {
		body.Add(card(widget.NewLabel("创建工作空间后，即可安排设备位置。")))
		return body
	}
	c := g.config.Clone()
	save := widget.NewButtonWithIcon("保存布局", theme.ConfirmIcon(), nil)
	save.Importance = widget.HighImportance
	save.Disable()
	board := newScreenBoard(c.Screens, c.Name, func() { save.Enable() })
	save.OnTapped = func() {
		next := c.Clone()
		next.Screens = append([]config.Screen(nil), board.screens...)
		g.applyFrom(c, next)
	}
	bottom := container.NewBorder(nil, nil, widget.NewLabel("拖动屏幕排列位置，点击保存后生效。"), save, nil)
	return container.NewBorder(body, container.NewVBox(space(16), bottom), nil, nil, card(container.NewScroll(board)))
}
