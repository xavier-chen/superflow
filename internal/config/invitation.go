package config

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"superflow.local/superflow/internal/i18n"
)

// Invitations are copied explicitly. Discovery packets never contain Code.
type Invitation struct {
	Name      string   `json:"name"`
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
	Code      string   `json:"code"`
}

func ValidAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	n, e := strconv.Atoi(port)
	return err == nil && e == nil && host != "" && !strings.ContainsAny(host, " /\\\t\r\n?#@") && n > 0 && n <= 65535
}

func (c *Config) Invitation(addresses []string) (string, error) {
	if c.Mode != "hub" {
		return "", errors.New(i18n.T("Create the invitation on the computer with the keyboard and mouse"))
	}
	code, err := c.Code()
	if err != nil {
		return "", err
	}
	i := Invitation{Name: c.Name, ID: c.ID, Addresses: addresses, Code: code}
	if err = i.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	return "superflow://connect/" + base64.RawURLEncoding.EncodeToString(b), nil
}

func (i Invitation) Validate() error {
	id, err := hex.DecodeString(i.ID)
	if !ValidName(i.Name) || err != nil || len(id) != 16 || len(i.Addresses) == 0 || len(i.Addresses) > 16 {
		return errors.New(i18n.T("The invitation is incomplete. Copy it again."))
	}
	for _, address := range i.Addresses {
		if !ValidAddress(address) {
			return errors.New(i18n.T("The invitation contains an invalid connection address"))
		}
	}
	if !strings.HasPrefix(i.Code, "SF2-") {
		return errors.New(i18n.T("The invitation contains an invalid pairing code"))
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(i.Code, "SF2-"))
	if err != nil || len(b) != 64 {
		return errors.New(i18n.T("The invitation contains an invalid pairing code"))
	}
	return nil
}

func ParseInvitation(value string) (Invitation, error) {
	var i Invitation
	if len(value) > 8192 {
		return i, errors.New(i18n.T("The invitation is too long"))
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "superflow" || u.Host != "connect" || u.RawQuery != "" || u.Fragment != "" {
		return i, errors.New(i18n.T("Paste the complete SuperFlow invitation"))
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Path, "/"))
	if err != nil || json.Unmarshal(b, &i) != nil {
		return i, errors.New(i18n.T("The invitation could not be read. Copy it again."))
	}
	return i, i.Validate()
}

// PlaceScreen preserves existing positions and places new authenticated devices
// next to the local desktop without requiring names to be entered at both ends.
func (c *Config) PlaceScreen(name string) bool {
	if !ValidName(name) || len(c.Screens) >= 64 {
		return false
	}
	localX, localY := 0, 0
	for _, screen := range c.Screens {
		if screen.Name == name {
			return false
		}
		if screen.Name == c.Name {
			localX, localY = screen.X, screen.Y
		}
	}
	direction := 1
	if c.Mode == "node" {
		direction = -1
	}
	for step := 1; step <= 65; step++ {
		x := localX + direction*step
		occupied := false
		for _, screen := range c.Screens {
			if screen.X == x && screen.Y == localY {
				occupied = true
				break
			}
		}
		if !occupied {
			c.Screens = append(c.Screens, Screen{Name: name, X: x, Y: localY})
			return true
		}
	}
	return false
}
