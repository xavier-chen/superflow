//go:build !windows

package clipboard

import "errors"

func newWindows() (Backend, error) { return nil, errors.New("Windows clipboard unavailable") }
