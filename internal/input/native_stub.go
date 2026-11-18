//go:build !darwin && !windows && !linux

package input

import (
	"errors"

	"superflow.local/superflow/internal/i18n"
)

func New(Options) (Driver, error) {
	return nil, errors.New(i18n.T("This build has no native input adapter. On macOS, build with CGO_ENABLED=1."))
}
