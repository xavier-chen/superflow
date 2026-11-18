//go:build desktop && linux

package desktop

import (
	"context"
	"github.com/godbus/dbus/v5"
	"time"
)

func hasTray() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var owner bool
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owner)
	return err == nil && owner
}
func trayInfrastructure() bool { _, err := dbus.SessionBus(); return err == nil }
