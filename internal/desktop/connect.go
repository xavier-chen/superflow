//go:build desktop

package desktop

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/discovery"
	"superflow.local/superflow/internal/input"
)

func explain(message string) *widget.Label {
	l := widget.NewLabel(message)
	l.Wrapping = fyne.TextWrapWord
	l.Importance = widget.LowImportance
	return l
}
func platformName(value string) string {
	switch value {
	case "darwin":
		return "Mac"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return value
}
func defaultName() string {
	name, _ := os.Hostname()
	name = strings.TrimSuffix(strings.TrimSpace(name), ".local")
	if !config.ValidName(name) {
		name = platformName(runtime.GOOS)
	}
	return name
}
func prepareConfig(mode, name string) (*config.Config, error) {
	c, err := config.New(mode, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	c.AutoLayout = true
	c.InputDevices = input.SuggestedDevices()
	c.ScreenWidth, c.ScreenHeight = input.DesktopSize()
	c.Input = input.Check(input.Options{Capture: mode == "hub", Devices: c.InputDevices, Width: c.ScreenWidth, Height: c.ScreenHeight}) == nil
	// Desktop sharing uses Fyne's native clipboard, not command-line utilities.
	c.Clipboard = true
	c.Audio.Codec = "auto"
	return c, nil
}
func (g *gui) setup() {
	var d *dialog.CustomDialog
	control := widget.NewButtonWithIcon("使用这台电脑的键盘和鼠标", theme.ComputerIcon(), func() { d.Hide(); g.createWorkspace() })
	control.Importance = widget.HighImportance
	join := widget.NewButtonWithIcon("连接另一台电脑", theme.NavigateNextIcon(), func() { d.Hide(); g.joinWorkspace() })
	content := container.NewVBox(explain("键盘鼠标接在哪台电脑？连接参数与屏幕布局会自动配置。"), space(16), control, explain("键盘鼠标接在这里，用它操作其他电脑。"), space(12), join, explain("键盘鼠标接在另一台电脑，在这里粘贴连接邀请。"))
	d = dialog.NewCustom("开始使用 SuperFlow", "稍后设置", content, g.window)
	d.Resize(fyne.NewSize(540, 390))
	d.Show()
}
func (g *gui) createWorkspace() {
	c, err := prepareConfig("hub", defaultName())
	if err != nil {
		g.showError(err)
		return
	}
	name := widget.NewEntry()
	name.SetText(c.Name)
	keys, clips := newToggle(c.Input), newToggle(c.Clipboard)
	hint := "完成后自动连接；之后打开应用将恢复共享。声音和麦克风可在连接后按需开启。"
	if !c.Input {
		hint = "键鼠权限尚未就绪，可以先连接设备。授权后在“键鼠与剪贴板”开启，声音和麦克风可稍后配置。"
	}
	advanced := widget.NewAccordion(widget.NewAccordionItem("设备名称", name))
	body := container.NewVBox(explain(hint), space(12), container.NewBorder(nil, nil, widget.NewLabel("键盘与鼠标"), keys, nil), container.NewBorder(nil, nil, widget.NewLabel("文本剪贴板"), clips, nil), space(8), advanced)
	var d *dialog.CustomDialog
	start := widget.NewButton("开始使用", func() {
		next := c.Clone()
		next.Name = strings.TrimSpace(name.Text)
		next.Screens[0].Name = next.Name
		next.Input = keys.Checked
		next.Clipboard = clips.Checked
		if err := next.Validate(); err != nil {
			g.showError(err)
			return
		}
		if !g.checkInput(next) {
			return
		}
		d.Hide()
		g.finishSetup(next, true)
	})
	start.Importance = widget.HighImportance
	d = dialog.NewCustomWithoutButtons("准备这台电脑", container.NewVBox(body, space(14), container.NewHBox(layout.NewSpacer(), widget.NewButton("返回", func() { d.Hide(); g.setup() }), start)), g.window)
	d.Resize(fyne.NewSize(540, 400))
	d.Show()
}
func (g *gui) finishSetup(c *config.Config, invite bool) {
	g.perform("已保存，正在连接设备", func() error {
		if err := g.service.Create(c); err != nil {
			return err
		}
		return g.service.Start()
	}, func() {
		g.app.Preferences().SetBool("start-sharing", true)
		g.showPage(0)
		g.service.Discover()
		if invite {
			g.addDevice()
		}
	})
}
func localAddresses(port string) []string {
	var out []string
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.To4() != nil && ip.IsPrivate() {
				out = append(out, net.JoinHostPort(ip.String(), port))
			}
		}
	}
	sort.Strings(out)
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}
func (g *gui) addDevice() {
	if g.config == nil {
		g.setup()
		return
	}
	if g.config.Mode != "hub" {
		dialog.ShowInformation("添加更多电脑", "请在连接键盘和鼠标的电脑上打开 SuperFlow，然后复制连接邀请。", g.window)
		return
	}
	c := g.config.Clone()
	address := c.Listen
	if g.status.ListenAddress != "" {
		address = g.status.ListenAddress
	}
	_, port, _ := net.SplitHostPort(address)
	if port == "" || port == "0" {
		port = "42800"
	}
	addresses := localAddresses(port)
	manual := widget.NewEntry()
	if len(addresses) > 0 {
		manual.SetText(addresses[0])
	} else {
		manual.SetPlaceHolder("192.168.1.10:" + port)
	}
	hint := explain("在另一台电脑打开 SuperFlow，选择“连接另一台电脑”，粘贴邀请即可。设备名称和屏幕位置会自动配置。")
	if !running(g.status.State) {
		hint.SetText("先开始共享，再在另一台电脑粘贴连接邀请。配对后会自动加入工作空间。")
	}
	copyInvite := widget.NewButtonWithIcon("复制连接邀请", theme.ContentCopyIcon(), func() {
		targets := addresses
		if len(targets) == 0 {
			targets = []string{strings.TrimSpace(manual.Text)}
		} else if custom := strings.TrimSpace(manual.Text); custom != "" && custom != targets[0] {
			targets = []string{custom}
			for _, address := range addresses {
				if address != custom && len(targets) < 16 {
					targets = append(targets, address)
				}
			}
		}
		invitation, err := c.Invitation(targets)
		if err != nil {
			g.showError(err)
			return
		}
		copy := func() {
			g.window.Clipboard().SetContent(invitation)
			g.message.SetText("邀请已复制，在另一台电脑粘贴即可")
		}
		if !c.AutoLayout {
			next := c.Clone()
			next.AutoLayout = true
			g.perform("已开启新设备自动布局", func() error {
				ctx, cancel := context.WithTimeout(g.ctx, 5*time.Second)
				defer cancel()
				return g.service.Apply(ctx, c, next)
			}, func() { c = g.config.Clone(); copy() })
		} else {
			copy()
		}
	})
	copyInvite.Importance = widget.HighImportance
	code := widget.NewButton("复制兼容配对码", func() {
		value, err := c.Code()
		if err == nil {
			g.window.Clipboard().SetContent(value)
		}
	})
	expert := widget.NewAccordion(widget.NewAccordionItem("手动连接", container.NewVBox(explain("自动发现被网络限制时可使用手动地址。兼容配对码用于旧版 Go 客户端。"), manual, code)))
	if len(addresses) == 0 {
		expert.Open(0)
	}
	content := container.NewVBox(hint, space(16), copyInvite, space(8), explain("连接邀请包含设备授权，请只交给你信任的电脑。"), space(12), expert)
	d := dialog.NewCustom("连接新设备", "完成", content, g.window)
	d.Resize(fyne.NewSize(550, 390))
	d.Show()
}
func (g *gui) joinWorkspace() {
	g.service.Discover()
	template, err := prepareConfig("node", defaultName())
	if err != nil {
		g.showError(err)
		return
	}
	keys, clips := newToggle(template.Input), newToggle(template.Clipboard)
	invite := widget.NewPasswordEntry()
	invite.SetPlaceHolder("粘贴 SuperFlow 连接邀请")
	paste := widget.NewButtonWithIcon("粘贴", theme.ContentPasteIcon(), func() { invite.SetText(strings.TrimSpace(g.window.Clipboard().Content())) })
	status := explain("邀请会自动填写连接地址，无需输入对方的设备名称。")
	nearby := widget.NewSelect([]string{"自动使用邀请中的设备"}, nil)
	nearby.SetSelectedIndex(0)
	peers := map[string]discovery.Peer{}
	name := widget.NewEntry()
	name.SetText(defaultName())
	server := widget.NewEntry()
	server.SetPlaceHolder("兼容旧配对码时填写地址")
	advanced := widget.NewAccordion(widget.NewAccordionItem("更多选项", container.NewVBox(widget.NewLabel("本机名称"), name, widget.NewLabel("手动地址"), server)))
	last := ""
	update := func() {
		found := g.service.Nearby()
		key := ""
		for _, p := range found {
			key += p.ID + p.Address + stringBool(p.Ready)
		}
		if key == last {
			return
		}
		last = key
		selected := nearby.Selected
		options := []string{"自动使用邀请中的设备"}
		peers = map[string]discovery.Peer{}
		seen := map[string]bool{}
		for _, p := range found {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			label := p.Name + " · " + platformName(p.OS) + " · " + p.ID[len(p.ID)-4:]
			if !p.Ready {
				label += "（未开始共享）"
			}
			options = append(options, label)
			peers[label] = p
		}
		nearby.Options = options
		nearby.Refresh()
		nearby.SetSelectedIndex(0)
		for _, option := range options {
			if option == selected {
				nearby.SetSelected(option)
				break
			}
		}
	}
	update()
	g.discoveryRefresh = update
	invite.OnChanged = func(value string) {
		if i, err := config.ParseInvitation(value); err == nil {
			status.SetText("将连接到 " + i.Name + "，配对后自动恢复连接。")
		} else {
			status.SetText("邀请会自动填写连接地址，无需输入对方的设备名称。")
		}
	}
	body := container.NewVBox(widget.NewLabel("连接邀请"), container.NewBorder(nil, nil, nil, paste, invite), status, space(12), widget.NewLabel("附近的电脑"), nearby, explain("找不到设备也可以直接粘贴邀请。两台电脑需处于可互通的网络。"), container.NewBorder(nil, nil, widget.NewLabel("键盘与鼠标"), keys, nil), container.NewBorder(nil, nil, widget.NewLabel("文本剪贴板"), clips, nil), explain("声音和麦克风保持关闭，连接后可按需开启。"), space(8), advanced)
	var d *dialog.CustomDialog
	connect := widget.NewButton("配对并连接", func() {
		c := template.Clone()
		c.Name = strings.TrimSpace(name.Text)
		c.Screens[0].Name = c.Name
		c.Input = keys.Checked
		c.Clipboard = clips.Checked
		value := strings.TrimSpace(invite.Text)
		address := strings.TrimSpace(server.Text)
		code := value
		hubName := ""
		if i, e := config.ParseInvitation(value); e == nil {
			code = i.Code
			address = i.Addresses[0]
			hubName = i.Name
			c.ServerAlternates = append([]string(nil), i.Addresses[1:]...)
			probe := c.Clone()
			if e = probe.Pair(code, address); e != nil {
				g.showError(e)
				return
			}
			for _, p := range g.service.Nearby() {
				if p.Pin == probe.Pin && p.Ready {
					address = p.Address
					break
				}
			}
		} else if !strings.HasPrefix(value, "SF2-") {
			g.showError(e)
			return
		}
		if peer, ok := peers[nearby.Selected]; ok {
			address = peer.Address
			hubName = peer.Name
		}
		if !config.ValidAddress(address) {
			g.showError(errors.New("请粘贴完整连接邀请，或选择附近的电脑并填写兼容配对码"))
			return
		}
		if err = c.Pair(code, address); err != nil {
			g.showError(err)
			return
		}
		if peer, ok := peers[nearby.Selected]; ok && peer.Pin != c.Pin {
			g.showError(errors.New("所选设备与邀请不匹配，请选择“自动使用邀请中的设备”"))
			return
		}
		if hubName == c.Name {
			c.Name += "-" + c.ID[:4]
			c.Screens[0].Name = c.Name
		}
		if hubName != "" {
			c.PlaceScreen(hubName)
		}
		if !g.checkInput(c) {
			return
		}
		d.Hide()
		g.finishSetup(c, false)
	})
	connect.Importance = widget.HighImportance
	d = dialog.NewCustomWithoutButtons("连接另一台电脑", container.NewVBox(body, space(14), container.NewHBox(layout.NewSpacer(), widget.NewButton("返回", func() { d.Hide(); g.setup() }), connect)), g.window)
	d.SetOnClosed(func() { g.discoveryRefresh = nil })
	d.Resize(fyne.NewSize(590, 620))
	d.Show()
}
func stringBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
func (g *gui) checkInput(c *config.Config) bool {
	if !c.Input {
		return true
	}
	if err := input.Check(input.Options{Capture: c.Mode == "hub", Devices: c.InputDevices, Width: c.ScreenWidth, Height: c.ScreenHeight}); err != nil {
		content := container.NewVBox(explain(err.Error()), explain("完成授权后可继续，也可以暂时关闭键鼠共享，先连接设备。"))
		if runtime.GOOS == "darwin" {
			content.Add(container.NewHBox(widget.NewButton("辅助功能", func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") }), widget.NewButton("输入监控", func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent") })))
		}
		d := dialog.NewCustom("还需要一步设置", "知道了", content, g.window)
		d.Resize(fyne.NewSize(540, 240))
		d.Show()
		return false
	}
	return true
}
