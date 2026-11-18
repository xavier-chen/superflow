//go:build !darwin && !linux && !windows

package instance

import (
	"errors"
	"os"
)

func tryLock(*os.File) (bool, error) { return false, errors.New("unsupported desktop platform") }
func unlock(*os.File)                {}
func allowForeground(int)            {}
