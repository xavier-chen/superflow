//go:build !darwin && !windows && !linux

package input

import (
	"errors"

	"superflow.local/superflow/internal/i18n"
)

func Check(Options) error { return errors.New(i18n.T("Input sharing is unavailable on this platform")) }
