//go:build !darwin && !windows && !linux

package input

import "errors"

func Check(Options) error { return errors.New("当前系统暂不支持键鼠共享") }
