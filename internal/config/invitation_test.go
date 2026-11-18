package config

import (
	"strings"
	"testing"
)

func TestInvitationPreservesPinnedIdentityAndAddresses(t *testing.T) {
	hub, err := New("hub", "我的电脑")
	if err != nil {
		t.Fatal(err)
	}
	addresses := []string{"192.168.1.10:42800", "10.0.0.2:42800"}
	value, err := hub.Invitation(addresses)
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := ParseInvitation(" \n" + value + "\n")
	if err != nil {
		t.Fatal(err)
	}
	node, _ := New("node", "另一台")
	if err = node.Pair(invitation.Code, invitation.Addresses[0]); err != nil {
		t.Fatal(err)
	}
	if node.Pin != hub.Pin || node.Secret != hub.Secret || invitation.Name != hub.Name || len(invitation.Addresses) != 2 {
		t.Fatal("invitation lost identity or routing")
	}
	for _, bad := range []string{strings.Replace(value, "superflow:", "https:", 1), value + "?secret=x", strings.Repeat("x", 9000), "superflow://connect/not-base64"} {
		if _, err := ParseInvitation(bad); err == nil {
			t.Fatal("accepted invalid invitation")
		}
	}
	for _, bad := range []string{"0.0.0.0:0", "192.168.1.1:99999", "host/path:42800", "user@host:42800"} {
		if _, err := hub.Invitation([]string{bad}); err == nil {
			t.Fatal("accepted malformed destination", bad)
		}
	}
}
func TestAutomaticPlacementPreservesUserLayout(t *testing.T) {
	c, _ := New("hub", "local")
	c.Screens = []Screen{{Name: "local", X: 3, Y: 2}, {Name: "existing", X: 4, Y: 2}}
	if !c.PlaceScreen("new") || c.Screens[2].X != 5 || c.Screens[2].Y != 2 {
		t.Fatal(c.Screens)
	}
	if c.PlaceScreen("existing") || c.Screens[1].X != 4 {
		t.Fatal("overwrote layout")
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFormSaveKeepsAutomaticLayoutAndLiveGain(t *testing.T) {
	base, _ := New("hub", "local")
	current := base.Clone()
	current.PlaceScreen("new-device")
	current.Audio.Gains = map[string]float64{"new-device": .5}
	edit := base.Clone()
	edit.Audio.BufferMS = 60
	result, err := MergeEdit(base, edit, current)
	if err != nil || len(result.Screens) != 2 || result.Audio.BufferMS != 60 || result.Audio.Gains["new-device"] != .5 {
		t.Fatal("form save lost background changes", result, err)
	}
	current.Audio.BufferMS = 80
	if _, err = MergeEdit(base, edit, current); err != ErrChanged {
		t.Fatal("overlapping edit was silently overwritten", err)
	}
}
