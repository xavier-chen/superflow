//go:build windows

package input

import (
	"errors"

	"superflow.local/superflow/internal/i18n"
)

func Check(Options) error {
	if dimension(78) <= 1 || dimension(79) <= 1 {
		return errors.New(i18n.T("Open SuperFlow in a normal desktop session"))
	}
	return nil
}
