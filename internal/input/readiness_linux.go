//go:build linux

package input

import (
	"errors"
	"fmt"
	"os"
)

func Check(o Options) error {
	if o.Width <= 0 || o.Height <= 0 {
		o.Width, o.Height = DesktopSize()
	}
	if o.Capture {
		if len(o.Devices) == 0 {
			return errors.New("尚未找到可用的物理键盘和鼠标，请连接设备或在屏幕布局的高级设置中选择")
		}
		for _, path := range o.Devices {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("键鼠设备尚未获得访问权限：%s", path)
			}
			f.Close()
		}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") == "" {
		f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
		if err != nil {
			return errors.New("当前桌面还未授予虚拟键鼠设备权限，请配置 /dev/uinput 的访问权限")
		}
		f.Close()
		if o.Capture && (o.Width <= 0 || o.Height <= 0) {
			return errors.New("Wayland 桌面需要先在高级布局设置中填写桌面尺寸")
		}
	}
	return nil
}
