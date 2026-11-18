//go:build desktop

package desktop

import (
	"fyne.io/fyne/v2"
	fynedesktop "fyne.io/fyne/v2/driver/desktop"
	"runtime"
)

func (g *gui) closeWindow() {
	if !g.quitting && g.app.Preferences().BoolWithFallback("close-to-tray", true) && (g.canRecover || g.trayAvailable) {
		g.window.Hide()
		g.visible.Store(false)
		return
	}
	g.quit()
}

// Fyne creates a native monitor window when the tray starts. Initialize it on
// the startup thread; Linux's notifier backend handles panel restarts itself.
func (g *gui) initializeTray() {
	if trayInfrastructure() && g.tray == nil {
		if tray, ok := g.app.(fynedesktop.App); ok {
			g.tray = tray
			tray.SetSystemTrayIcon(systemTrayIcon())
			tray.SetSystemTrayWindow(g.window)
			installTrayActivation(g.reveal)
			g.window.SetCloseIntercept(g.closeWindow)
			g.lastTray = ""
			g.updateTray()
		}
	}
	g.syncTray(hasTray())
}
func (g *gui) syncTray(available bool) {
	g.trayAvailable = available
	if g.backgroundHint != nil {
		g.backgroundHint.SetText(g.backgroundDescription())
	}
}
func (g *gui) backgroundDescription() string {
	if g.trayAvailable {
		return "关闭后继续共享。点击托盘图标或再次打开 SuperFlow，即可返回窗口。"
	}
	if g.canRecover {
		return "当前桌面未提供托盘。关闭后继续共享，再次打开 SuperFlow 即可返回窗口。"
	}
	return "关闭窗口后会退出应用。"
}
func (g *gui) installShortcuts() {
	modifier := fyne.KeyModifierControl
	if runtime.GOOS == "darwin" {
		modifier = fyne.KeyModifierSuper
	}
	g.window.Canvas().AddShortcut(&fynedesktop.CustomShortcut{KeyName: fyne.KeyQ, Modifier: modifier}, func(fyne.Shortcut) { g.quit() })
	g.window.Canvas().AddShortcut(&fynedesktop.CustomShortcut{KeyName: fyne.KeyComma, Modifier: modifier}, func(fyne.Shortcut) { g.showPage(5) })
}
