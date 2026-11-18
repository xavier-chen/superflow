//go:build !darwin && !windows && !linux

package audio

import (
	"context"
	"errors"
	"io"
)

func Devices() ([]Device, error) { return nil, errors.New("unsupported audio platform") }
func NativeCapture(context.Context, io.Writer, string, bool) error {
	return errors.New("unsupported audio platform")
}
func NativePlayback(context.Context, io.Reader, string) error {
	return errors.New("unsupported audio platform")
}
