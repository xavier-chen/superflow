//go:build linux

package input

import (
	"errors"
	"fmt"
	"os"

	"superflow.local/superflow/internal/i18n"
)

func Check(o Options) error {
	if o.Width <= 0 || o.Height <= 0 {
		o.Width, o.Height = DesktopSize()
	}
	if o.Capture {
		if len(o.Devices) == 0 {
			return errors.New(i18n.T("No physical keyboard or mouse detected. Connect one or choose its path in the advanced layout settings."))
		}
		for _, path := range o.Devices {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf(i18n.T("No permission to access input device: %s"), path)
			}
			f.Close()
		}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") == "" {
		f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
		if err != nil {
			return errors.New(i18n.T("Grant access to /dev/uinput to enable virtual keyboard and mouse input"))
		}
		f.Close()
		if o.Capture && (o.Width <= 0 || o.Height <= 0) {
			return errors.New(i18n.T("Enter the desktop dimensions in the advanced layout settings for Wayland"))
		}
	}
	return nil
}
