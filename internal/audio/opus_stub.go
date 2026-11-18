//go:build !darwin && !linux && !windows

package audio

import "errors"

func openOpus() (uintptr, error) { return 0, errors.New("unsupported codec platform") }
