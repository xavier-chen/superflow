// Package integration installs only this application's files for the current
// user. It never installs a driver or changes system-wide permissions.
package integration

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"superflow.local/superflow/internal/brand"
)

type locations struct{ Executable, Launcher, Autostart, Icon string }

func currentLocations() (locations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return locations{}, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		data = filepath.Join(home, ".local", "share")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(config) {
		config = filepath.Join(home, ".config")
	}
	roaming, err := os.UserConfigDir()
	if err != nil {
		return locations{}, err
	}
	local := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(local) {
		local = filepath.Join(home, "AppData", "Local")
	}
	return pathsFor(runtime.GOOS, home, data, config, roaming, local), nil
}
func pathsFor(platform, home, data, config, roaming, local string) locations {
	switch platform {
	case "windows":
		return locations{Executable: filepath.Join(local, "SuperFlow", "SuperFlow.exe"), Launcher: filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs", "SuperFlow.lnk")}
	case "darwin":
		return locations{Executable: filepath.Join(home, "Applications", "SuperFlow.app", "Contents", "MacOS", "SuperFlow"), Autostart: filepath.Join(home, "Library", "LaunchAgents", brand.ApplicationID+".plist")}
	default:
		return locations{Executable: filepath.Join(data, "superflow", "superflow"), Launcher: filepath.Join(data, "applications", brand.ApplicationID+".desktop"), Autostart: filepath.Join(config, "autostart", brand.ApplicationID+".desktop"), Icon: filepath.Join(data, "icons", "hicolor", "256x256", "apps", brand.ApplicationID+".png")}
	}
}
func Install(profiles ...string) (string, error) {
	source, err := os.Executable()
	if err != nil {
		return "", err
	}
	target, err := currentLocations()
	if err != nil {
		return "", err
	}
	profile := ""
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	return installAt(runtime.GOOS, source, target, profile)
}
func installAt(platform, source string, target locations, profile string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(source); err == nil {
		source = resolved
	}
	if platform == "darwin" {
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(source)))
		if !strings.HasSuffix(bundle, ".app") {
			return "", errors.New("请使用打包后的 SuperFlow.app 安装")
		}
		destination := filepath.Dir(filepath.Dir(filepath.Dir(target.Executable)))
		if filepath.Clean(bundle) != filepath.Clean(destination) {
			err := filepath.WalkDir(bundle, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				relative, _ := filepath.Rel(bundle, path)
				dest := filepath.Join(destination, relative)
				if entry.Type()&os.ModeSymlink != 0 {
					return errors.New("应用包中存在不支持的符号链接")
				}
				if entry.IsDir() {
					return os.MkdirAll(dest, 0755)
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				return copyAtomic(path, dest, info.Mode().Perm())
			})
			if err != nil {
				return "", err
			}
		}
		return target.Executable, nil
	}
	if filepath.Clean(source) != filepath.Clean(target.Executable) {
		if err := copyAtomic(source, target.Executable, 0755); err != nil {
			return "", err
		}
	}
	for _, relative := range []string{"opus.dll", filepath.Join("lib", "libopus.so.0"), "Opus-LICENSE.txt", "THIRD_PARTY_NOTICES.txt"} {
		src := filepath.Join(filepath.Dir(source), relative)
		if info, err := os.Stat(src); err == nil && info.Mode().IsRegular() {
			dest := filepath.Join(filepath.Dir(target.Executable), relative)
			if filepath.Clean(src) != filepath.Clean(dest) {
				if err = copyAtomic(src, dest, info.Mode().Perm()); err != nil {
					return "", err
				}
			}
		}
	}
	if platform == "windows" {
		if err := writeShortcut(target.Launcher, target.Executable, launchArguments(profile, false)); err != nil {
			return "", err
		}
	} else {
		icon, err := brand.PNG(256)
		if err != nil {
			return "", err
		}
		if err = writeAtomic(target.Icon, icon, 0644); err != nil {
			return "", err
		}
		entry, err := desktopEntry(target.Executable, target.Icon, false, profile)
		if err != nil {
			return "", err
		}
		if err = writeAtomic(target.Launcher, []byte(entry), 0644); err != nil {
			return "", err
		}
	}
	return target.Executable, nil
}
func desktopEntry(executable, icon string, background bool, profiles ...string) (string, error) {
	quote := func(value string) (string, error) {
		if strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("应用路径包含不支持的字符")
		}
		value = strings.ReplaceAll(value, "\\", "\\\\\\\\")
		value = strings.ReplaceAll(value, "%", "%%")
		for _, c := range []string{"\"", "`", "$"} {
			value = strings.ReplaceAll(value, c, "\\\\"+c)
		}
		return "\"" + value + "\"", nil
	}
	command, err := quote(executable)
	if err != nil {
		return "", err
	}
	command += " ui"
	if background {
		command += " --background"
	}
	if len(profiles) > 0 && profiles[0] != "" {
		profile, err := quote(profiles[0])
		if err != nil {
			return "", err
		}
		command += " --config " + profile
	}
	if strings.ContainsAny(icon, "\r\n\x00") {
		return "", errors.New("图标路径包含不支持的字符")
	}
	text := "[Desktop Entry]\nType=Application\nVersion=1.0\nName=SuperFlow\nComment=Share audio, keyboard, mouse and clipboard\nExec=" + command + "\nIcon=" + icon + "\nTerminal=false\nCategories=Utility;Network;\nStartupNotify=true\nStartupWMClass=SuperFlow\n"
	if background {
		text += "X-GNOME-Autostart-enabled=true\nX-SuperFlow-Autostart=true\n"
	}
	return text, nil
}

func launchArguments(profile string, background bool) string {
	args := "ui"
	if background {
		args += " --background"
	}
	if profile != "" {
		args += " --config " + quoteWindows(profile)
	}
	return args
}
func quoteWindows(value string) string { return "\"" + value + "\"" }
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".superflow-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func copyAtomic(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(destination), ".superflow-*")
	if err != nil {
		return err
	}
	name := out.Name()
	defer os.Remove(name)
	if err = out.Chmod(mode); err == nil {
		_, err = io.Copy(out, in)
	}
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, destination); err != nil {
		return fmt.Errorf("无法更新应用文件，请退出旧版本后重试：%w", err)
	}
	return nil
}
