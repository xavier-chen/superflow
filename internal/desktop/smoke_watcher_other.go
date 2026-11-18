//go:build desktop && smoke && !linux

package desktop

import (
	"context"
	"errors"
)

type nativeWatcher interface {
	Close()
	Wait(context.Context) error
	Activate(context.Context) error
	Restart(context.Context) error
}

func testWatcher(enabled bool) (nativeWatcher, error) {
	if enabled {
		return nil, errors.New("test StatusNotifierWatcher requires Linux")
	}
	return nil, nil
}
