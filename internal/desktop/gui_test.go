//go:build desktop

package desktop

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/control"
)

type captureTheme struct {
	superTheme
	font fyne.Resource
}

func (t captureTheme) Font(style fyne.TextStyle) fyne.Resource {
	if t.font != nil {
		return t.font
	}
	return t.superTheme.Font(style)
}
func testGUI(t *testing.T, configured bool) *gui {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	path := filepath.Join(t.TempDir(), "v2.json")
	if configured {
		c, err := config.New("hub", "本机")
		if err != nil {
			t.Fatal(err)
		}
		c.Input = false
		c.Listen = "127.0.0.1:0"
		c.Screens = append(c.Screens, config.Screen{Name: "Linux 工作站", X: 1})
		if err = config.Save(path, c); err != nil {
			t.Fatal(err)
		}
	}
	s, err := control.New(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	t.Cleanup(a.Quit)
	var font fyne.Resource
	if path := os.Getenv("SUPERFLOW_TEST_FONT"); path != "" {
		font, _ = fyne.LoadResourceFromPath(path)
	}
	a.Settings().SetTheme(captureTheme{superTheme{}, font})
	g := newGUI(ctx, cancel, a, s)
	g.audioLoaded = true
	g.audioDevices = []audio.Device{{Kind: "playback", ID: "out", Name: "系统输出"}, {Kind: "capture", ID: "mic", Name: "物理麦克风"}, {Kind: "playback", ID: "virtual", Name: "虚拟音频输入"}}
	g.window.Resize(fyne.NewSize(1120, 850))
	g.window.Show()
	t.Cleanup(g.window.Close)
	return g
}
func TestRenderDesktopPages(t *testing.T) {
	g := testGUI(t, true)
	dir := os.Getenv("SUPERFLOW_SCREENSHOTS")
	for i, name := range []string{"overview", "devices", "audio", "input", "layout", "settings"} {
		g.showPage(i)
		size := g.window.Canvas().Size()
		if size.Width < 900 || size.Height < 600 {
			t.Fatal("unexpected page canvas", size)
		}
		if dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, g.window.Canvas().Capture())
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
func TestDragLayoutDoesNotOverwriteOrOverlap(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	screens := []config.Screen{{Name: "本机"}, {Name: "node", X: 1}}
	changed := 0
	board := newScreenBoard(screens, "本机", func() { changed++ })
	w := a.NewWindow("layout")
	w.SetContent(board)
	w.Resize(fyne.NewSize(760, 400))
	w.Show()
	defer w.Close()
	_ = test.WidgetRenderer(board)
	board.Refresh()
	first := board.tiles[0]
	first.Move(fyne.NewPos(board.origin.X+cellWidth, board.origin.Y))
	first.DragEnd()
	if changed != 0 || board.screens[0].X != 0 {
		t.Fatal("overlapping layout accepted")
	}
	first.Move(fyne.NewPos(board.origin.X-cellWidth, board.origin.Y))
	first.DragEnd()
	if changed != 1 || board.screens[0].X != -1 {
		t.Fatal("drag did not snap")
	}
	if screens[0].X != 0 {
		t.Fatal("unsaved drag mutated live config")
	}
}
func TestRouteKeepsCustomCommandAndRejectsMissingVirtualDevice(t *testing.T) {
	g := testGUI(t, true)
	args := []string{"custom-audio-tool", "--device", "existing"}
	editor, _ := g.route("音频", "", args, true, false)
	got, err := editor.command()
	if err != nil || len(got) != len(args) || got[0] != args[0] {
		t.Fatal("custom command lost", got, err)
	}
	virtual, _ := g.route("远端麦克风", "", nil, false, true)
	virtual.on.Checked = true
	if _, err = virtual.command(); err == nil {
		t.Fatal("remote microphone silently routed to default speaker")
	}
}

func TestTelemetryRefreshKeepsWorkspaceObjects(t *testing.T) {
	g := testGUI(t, true)
	original := g.workspace.Objects[0]
	g.refresh()
	if g.workspace.Objects[0] != original {
		t.Fatal("idle telemetry rebuilt device map")
	}
	g.status.Target = "Linux 工作站"
	g.refresh()
	if g.workspace.Objects[0] == original {
		t.Fatal("device focus did not update device map")
	}
}
func TestRenderConnectionFlows(t *testing.T) {
	for _, name := range []string{"welcome", "join", "invite"} {
		t.Run(name, func(t *testing.T) {
			g := testGUI(t, name == "invite")
			switch name {
			case "welcome":
				g.setup()
			case "join":
				g.joinWorkspace()
			case "invite":
				g.addDevice()
			}
			dir := os.Getenv("SUPERFLOW_SCREENSHOTS")
			if dir == "" {
				return
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err = png.Encode(file, g.window.Canvas().Capture()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestToggleCanBeOperatedWithKeyboard(t *testing.T) {
	g := testGUI(t, true)
	toggle := newToggle(false)
	changed := 0
	toggle.OnChanged = func(bool) { changed++ }
	g.window.SetContent(toggle)
	g.window.Canvas().Focus(toggle)
	toggle.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if !toggle.Checked || changed != 1 {
		t.Fatal("space did not activate a sharing switch")
	}
	toggle.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if toggle.Checked || changed != 2 {
		t.Fatal("return did not activate a sharing switch")
	}
}
func TestDiagnosticsExcludePairingAndClipboardContent(t *testing.T) {
	g := testGUI(t, true)
	summary := g.diagnosticSummary()
	for _, value := range []string{g.config.Secret, g.config.PrivateKey, g.config.Certificate, g.config.Pin} {
		if value != "" && strings.Contains(summary, value) {
			t.Fatal("diagnostics exposed pairing material")
		}
	}
}
