//go:build desktop && smoke && linux

package desktop

import (
	"context"
	"errors"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"reflect"
	"strings"
	"sync"
	"time"
)

type nativeWatcher interface {
	Close()
	Wait(context.Context) error
	Activate(context.Context) error
	Restart(context.Context) error
}
type statusWatcher struct {
	conn       *dbus.Conn
	mu         sync.Mutex
	sender     string
	path       dbus.ObjectPath
	registered chan struct{}
}

func testWatcher(enabled bool) (nativeWatcher, error) {
	if !enabled {
		if hasTray() {
			return nil, errors.New("the no-tray check requires an isolated session without a watcher")
		}
		return nil, nil
	}
	if hasTray() {
		return nil, errors.New("refusing to replace an existing desktop panel")
	}
	watcher := &statusWatcher{registered: make(chan struct{}, 1)}
	if err := watcher.open(); err != nil {
		return nil, err
	}
	return watcher, nil
}
func (w *statusWatcher) open() error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	w.conn = conn
	if err = conn.Export(w, "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher"); err != nil {
		conn.Close()
		return err
	}
	_, err = prop.Export(conn, "/StatusNotifierWatcher", map[string]map[string]*prop.Prop{"org.kde.StatusNotifierWatcher": {
		"IsStatusNotifierHostRegistered": {Value: true, Emit: prop.EmitTrue}, "ProtocolVersion": {Value: int32(0)}, "RegisteredStatusNotifierItems": {Value: []string{}, Emit: prop.EmitTrue},
	}})
	if err != nil {
		conn.Close()
		return err
	}
	reply, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return errors.New("cannot own isolated tray watcher")
	}
	return nil
}
func (w *statusWatcher) RegisterStatusNotifierItem(service string, sender dbus.Sender) *dbus.Error {
	w.mu.Lock()
	w.sender = service
	w.path = "/StatusNotifierItem"
	if strings.HasPrefix(service, "/") {
		w.sender = string(sender)
		w.path = dbus.ObjectPath(service)
	}
	w.mu.Unlock()
	select {
	case w.registered <- struct{}{}:
	default:
	}
	return nil
}
func (w *statusWatcher) RegisterStatusNotifierHost(string) *dbus.Error { return nil }
func (w *statusWatcher) Close() {
	if w.conn != nil {
		_ = w.conn.Close()
	}
}
func (w *statusWatcher) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.registered:
	}
	w.mu.Lock()
	sender, path := w.sender, w.path
	w.mu.Unlock()
	var icon dbus.Variant
	if err := w.conn.Object(sender, path).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.kde.StatusNotifierItem", "IconPixmap").Store(&icon); err != nil {
		return err
	}
	value := reflect.ValueOf(icon.Value())
	if value.Kind() != reflect.Slice || value.Len() == 0 {
		return errors.New("native tray icon is empty")
	}
	return nil
}
func (w *statusWatcher) Activate(ctx context.Context) error {
	w.mu.Lock()
	sender, path := w.sender, w.path
	w.mu.Unlock()
	return w.conn.Object(sender, path).CallWithContext(ctx, "org.kde.StatusNotifierItem.Activate", 0, int32(0), int32(0)).Err
}
func (w *statusWatcher) Restart(ctx context.Context) error {
	w.Close()
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	if err := w.open(); err != nil {
		return err
	}
	return w.Wait(ctx)
}
