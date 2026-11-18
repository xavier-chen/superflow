// Package assist turns common user intentions into validated configuration.
package assist

import (
	"errors"
	"strings"

	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/config"
)

func VirtualDevice(name string) bool {
	name = strings.ToLower(name)
	for _, marker := range []string{"blackhole", "vb-audio", "cable input", "cable output", "superflow virtual", "loopback audio"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}
func UniqueVirtual(devices []audio.Device, kind string) string {
	id := ""
	for _, device := range devices {
		if device.Kind == kind && VirtualDevice(device.Name) {
			if id != "" {
				return ""
			}
			id = device.ID
		}
	}
	return id
}
func AudioPreset(c *config.Config, profile, platform string, devices []audio.Device) (*config.Config, error) {
	if c == nil {
		return nil, errors.New("请先连接设备")
	}
	next := c.Clone()
	switch profile {
	case "receive":
		next.Audio.PlaySystem = []string{"@self", "audio-play"}
		next.Audio.SendSystem = nil
	case "send":
		args := []string{"@self", "audio-capture", "--loopback"}
		if platform == "darwin" {
			device := UniqueVirtual(devices, "capture")
			if device == "" {
				return nil, errors.New("系统声音需要虚拟音频输入。请先安装或选择已有的虚拟设备；麦克风不受影响。")
			}
			args = []string{"@self", "audio-capture", "--device", device}
		}
		next.Audio.SendSystem = args
		next.Audio.PlaySystem = nil
	case "off":
		next.Audio.SendSystem = nil
		next.Audio.PlaySystem = nil
		next.Audio.SendMicrophone = nil
		next.Audio.PlayMicrophone = nil
	default:
		return nil, errors.New("未知音频快捷设置")
	}
	next.Audio.Codec = "auto"
	return next, next.Validate()
}
