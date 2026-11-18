//go:build windows

package input

import "errors"

func Check(Options) error {
	if dimension(78) <= 1 || dimension(79) <= 1 {
		return errors.New("当前会话没有可用的桌面，请在普通桌面会话中运行")
	}
	return nil
}
