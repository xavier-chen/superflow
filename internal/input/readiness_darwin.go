//go:build darwin

package input

import (
	"errors"

	"superflow.local/superflow/internal/i18n"
)

func Check(o Options) error {
	if err := loadMac(); err != nil {
		return err
	}
	if !mac.trusted() {
		return errors.New(i18n.T("Allow SuperFlow under Accessibility in System Settings, then reopen the app"))
	}
	if o.Capture && !mac.listenAccess() {
		return errors.New(i18n.T("Allow SuperFlow under Input Monitoring in System Settings, then reopen the app"))
	}
	return nil
}
