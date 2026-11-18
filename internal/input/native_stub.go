//go:build !darwin && !windows && !linux

package input

import "errors"

func New(Options) (Driver, error) {
	return nil, errors.New("当前构建不包含本机键鼠适配器；macOS 需要 CGO_ENABLED=1")
}
