//go:build darwin

package input

import "errors"

func Check(o Options) error {
	if err := loadMac(); err != nil {
		return err
	}
	if !mac.trusted() {
		return errors.New("请先在系统设置中允许 SuperFlow 使用辅助功能，然后重新打开应用")
	}
	if o.Capture && !mac.listenAccess() {
		return errors.New("请先在系统设置中允许 SuperFlow 使用输入监控，然后重新打开应用")
	}
	return nil
}
