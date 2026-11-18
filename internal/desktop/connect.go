//go:build desktop

package desktop

import (
	"context"
	"errors"
	"fmt"
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
	control := widget.NewButtonWithIcon(tr("Use this keyboard and mouse"), theme.ComputerIcon(), func() { d.Hide(); g.createWorkspace() })
	control.Importance = widget.HighImportance
	join := widget.NewButtonWithIcon(tr("Connect to another computer"), theme.NavigateNextIcon(), func() { d.Hide(); g.joinWorkspace() })
	content := container.NewVBox(explain(tr("Where are your keyboard and mouse connected? SuperFlow sets up the connection and screen layout automatically.")), space(16), control, explain(tr("Use these devices to control your other computers.")), space(12), join, explain(tr("Paste an invitation from the computer with the keyboard and mouse.")))
	language := g.languagePicker(func() { d.Hide(); g.setup() })
	content.Add(space(8))
	content.Add(container.NewBorder(nil, nil, widget.NewLabel(tr("Language")), language, nil))
	d = dialog.NewCustom(tr("Get started with SuperFlow"), tr("Set up later"), content, g.window)
	d.Resize(fyne.NewSize(540, 440))
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
	hint := tr("Sharing starts automatically and resumes when you reopen SuperFlow. Enable audio or microphone sharing after connecting.")
	if !c.Input {
		hint = tr("You can pair before granting input permissions. Once granted, turn on sharing in Input & clipboard.")
	}
	advanced := widget.NewAccordion(widget.NewAccordionItem(tr("Device name"), name))
	body := container.NewVBox(explain(hint), space(12), container.NewBorder(nil, nil, widget.NewLabel(tr("Keyboard and mouse")), keys, nil), container.NewBorder(nil, nil, widget.NewLabel(tr("Text clipboard")), clips, nil), space(8), advanced)
	var d *dialog.CustomDialog
	start := widget.NewButton(tr("Get started"), func() {
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
	d = dialog.NewCustomWithoutButtons(tr("Set up this computer"), container.NewVBox(body, space(14), container.NewHBox(layout.NewSpacer(), widget.NewButton(tr("Back"), func() { d.Hide(); g.setup() }), start)), g.window)
	d.Resize(fyne.NewSize(540, 400))
	d.Show()
}
func (g *gui) finishSetup(c *config.Config, invite bool) {
	g.perform("Settings saved. Connecting devices…", func() error {
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
		d := dialog.NewInformation(tr("Add more computers"), tr("On the computer with the keyboard and mouse, open SuperFlow and copy a connection invitation."), g.window)
		d.SetDismissText(tr("OK"))
		d.Show()
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
	hint := explain(tr("On the other computer, select “Connect to another computer” and paste the invitation. Device names and screen positions are set automatically."))
	if !running(g.status.State) {
		hint.SetText(tr("Start sharing, then paste the invitation on the other computer. It joins the workspace when paired."))
	}
	copyInvite := widget.NewButtonWithIcon(tr("Copy connection invitation"), theme.ContentCopyIcon(), func() {
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
			g.message.SetText(tr("Invitation copied. Paste it on the other computer."))
		}
		if !c.AutoLayout {
			next := c.Clone()
			next.AutoLayout = true
			g.perform("Automatic layout enabled for new devices", func() error {
				ctx, cancel := context.WithTimeout(g.ctx, 5*time.Second)
				defer cancel()
				return g.service.Apply(ctx, c, next)
			}, func() { c = g.config.Clone(); copy() })
		} else {
			copy()
		}
	})
	copyInvite.Importance = widget.HighImportance
	code := widget.NewButton(tr("Copy pairing code"), func() {
		value, err := c.Code()
		if err == nil {
			g.window.Clipboard().SetContent(value)
		}
	})
	expert := widget.NewAccordion(widget.NewAccordionItem(tr("Manual connection"), container.NewVBox(explain(tr("Use a manual address when network discovery is unavailable. You can also connect using a pairing code.")), manual, code)))
	if len(addresses) == 0 {
		expert.Open(0)
	}
	content := container.NewVBox(hint, space(16), copyInvite, space(8), explain(tr("Share the invitation only with computers you trust. It grants access to this workspace.")), space(12), expert)
	d := dialog.NewCustom(tr("Connect a new device"), tr("Done"), content, g.window)
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
	invite.SetPlaceHolder(tr("Paste a SuperFlow invitation"))
	paste := widget.NewButtonWithIcon(tr("Paste"), theme.ContentPasteIcon(), func() { invite.SetText(strings.TrimSpace(g.window.Clipboard().Content())) })
	status := explain(tr("The invitation fills in the address and device name for you."))
	nearby := newSelect([]string{tr("Use the device in the invitation")}, nil)
	nearby.SetSelectedIndex(0)
	peers := map[string]discovery.Peer{}
	name := widget.NewEntry()
	name.SetText(defaultName())
	server := widget.NewEntry()
	server.SetPlaceHolder(tr("Address for pairing code"))
	advanced := widget.NewAccordion(widget.NewAccordionItem(tr("More options"), container.NewVBox(widget.NewLabel(tr("This computer's name")), name, widget.NewLabel(tr("Manual address")), server)))
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
		options := []string{tr("Use the device in the invitation")}
		peers = map[string]discovery.Peer{}
		seen := map[string]bool{}
		for _, p := range found {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			label := p.Name + " · " + platformName(p.OS) + " · " + p.ID[len(p.ID)-4:]
			if !p.Ready {
				label += tr(" (sharing is paused)")
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
			status.SetText(fmt.Sprintf(tr("Connecting to %s. SuperFlow will reconnect automatically after pairing."), i.Name))
		} else {
			status.SetText(tr("The invitation fills in the address and device name for you."))
		}
	}
	body := container.NewVBox(widget.NewLabel(tr("Connection invitation")), container.NewBorder(nil, nil, nil, paste, invite), status, space(12), widget.NewLabel(tr("Nearby computers")), nearby, explain(tr("You can paste an invitation even if the computer is not listed. Both computers must be able to reach each other.")), container.NewBorder(nil, nil, widget.NewLabel(tr("Keyboard and mouse")), keys, nil), container.NewBorder(nil, nil, widget.NewLabel(tr("Text clipboard")), clips, nil), explain(tr("You can enable system audio and microphones after connecting.")), space(8), advanced)
	var d *dialog.CustomDialog
	connect := widget.NewButton(tr("Pair and connect"), func() {
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
			g.showError(errors.New(tr("Paste a full invitation or select a nearby computer and enter a pairing code")))
			return
		}
		if err = c.Pair(code, address); err != nil {
			g.showError(err)
			return
		}
		if peer, ok := peers[nearby.Selected]; ok && peer.Pin != c.Pin {
			g.showError(errors.New(tr("The selected device does not match the invitation. Choose “Use the device in the invitation”.")))
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
	d = dialog.NewCustomWithoutButtons(tr("Connect to another computer"), container.NewVBox(body, space(14), container.NewHBox(layout.NewSpacer(), widget.NewButton(tr("Back"), func() { d.Hide(); g.setup() }), connect)), g.window)
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
		content := container.NewVBox(explain(err.Error()), explain(tr("Grant permission to continue, or turn off input sharing for now and connect your devices.")))
		if runtime.GOOS == "darwin" {
			content.Add(container.NewHBox(widget.NewButton(tr("Accessibility"), func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") }), widget.NewButton(tr("Input Monitoring"), func() { g.openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent") })))
		}
		d := dialog.NewCustom(tr("One more step"), tr("Got it"), content, g.window)
		d.Resize(fyne.NewSize(540, 240))
		d.Show()
		return false
	}
	return true
}
