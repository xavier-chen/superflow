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
	"superflow.local/superflow/internal/input"
	"superflow.local/superflow/internal/integration"
)

func (g *gui) settingsPage() fyne.CanvasObject {
	header := g.heading("设置", "应用行为、系统检查与安装", nil)
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
		g.perform("登录启动设置已保存，下次登录生效", func() error {
			err := integration.SetAutoStart(value, g.service.Path())
			fyne.Do(func() { login.Checked = integration.AutoStartEnabled(); login.Refresh() })
			return err
		}, nil)
	}
	g.backgroundHint = explain(g.backgroundDescription())
	behavior := card(container.NewVBox(text("应用行为", 17, textColor, true), space(10), container.NewBorder(nil, nil, widget.NewLabel("关闭窗口后继续运行"), background, nil), g.backgroundHint, space(4), container.NewBorder(nil, nil, widget.NewLabel("打开应用时自动恢复连接"), auto, nil), container.NewBorder(nil, nil, widget.NewLabel("登录后在后台启动"), login, nil), explain("登录启动会先安装到当前用户的应用目录，不需要管理员权限。")))
	status := explain("正在检查系统…")
	g.supportStatus = status
	refresh := widget.NewButtonWithIcon("重新检查", theme.ViewRefreshIcon(), func() { g.checkSystem(status) })
	detect := widget.NewButton("重新检测键鼠", func() {
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
	tools := container.NewHBox(refresh, detect, widget.NewButton("声音设备", func() { g.openSoundSettings(false) }), widget.NewButton("麦克风权限", func() { g.openSoundSettings(true) }))
	system := card(container.NewVBox(text("系统检查", 17, textColor, true), status, tools))
	install := widget.NewButtonWithIcon("安装到本机", theme.DownloadIcon(), func() {
		var destination string
		g.perform("应用已安装，可以从应用菜单再次打开", func() error { var err error; destination, err = integration.Install(g.service.Path()); return err }, func() { g.message.SetText("已安装：" + destination) })
	})
	copy := widget.NewButtonWithIcon("复制诊断信息", theme.ContentCopyIcon(), func() {
		g.window.Clipboard().SetContent(g.diagnosticSummary())
		g.message.SetText("诊断信息已复制，不包含配对密钥或剪贴板内容")
	})
	installCard := card(container.NewVBox(text("安装与支持", 17, textColor, true), explain("三台电脑使用相同的连接邀请和共享设置。安装后可通过应用菜单唤回窗口，已有配对会保留。"), container.NewHBox(install, copy), widget.NewButton("高级配置", g.advancedSettings)))
	about := container.NewHBox(text("SuperFlow", 14, textColor, true), text(core.Release, 11, mutedColor, false))
	g.checkSystem(status)
	return container.NewVScroll(container.NewVBox(header, space(20), behavior, space(12), system, space(12), installCard, space(12), about))
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
		keys := "键鼠已就绪"
		if err := input.Check(options); err != nil {
			keys = "键鼠需要设置：" + err.Error()
		}
		codec := "高效音频可用"
		if audio.OpusAvailable() != nil {
			codec = "兼容音频可用（自动使用 PCM）"
		}
		message := keys + "\n原生剪贴板已就绪 · " + codec
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
		g.showError(fmt.Errorf("请在桌面的系统设置中打开“声音”，选择要使用的输入和输出设备"))
	}
}
func (g *gui) diagnosticSummary() string {
	sharing := "停止"
	if running(g.status.State) {
		sharing = "运行中"
	}
	return strings.Join([]string{"SuperFlow " + core.Release, "系统：" + runtime.GOOS + "/" + runtime.GOARCH, "共享：" + sharing, fmt.Sprintf("在线设备：%d", len(g.status.Peers)), fmt.Sprintf("托盘：%t", g.trayAvailable), "剪贴板：原生桌面接口"}, "\n")
}
