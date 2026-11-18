//go:build desktop && !linux

package desktop

func hasTray() bool            { return true }
func trayInfrastructure() bool { return true }
