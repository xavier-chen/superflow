//go:build linux

package input

import (
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"os"
)

// Xwayland exposes the compositor's desktop geometry as well as X11. This is
// read-only and never injects or captures an input event.
func DesktopSize() (int, int) {
	if os.Getenv("DISPLAY") == "" {
		return 0, 0
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return 0, 0
	}
	defer conn.Close()
	screen := xproto.Setup(conn).DefaultScreen(conn)
	return int(screen.WidthInPixels), int(screen.HeightInPixels)
}
