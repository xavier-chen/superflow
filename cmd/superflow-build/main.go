// superflow-build builds the Go core or a native, packaged desktop application.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type buildOptions struct {
	notices                      []byte
	Desktop                      bool
	Out, OS, Arch, Opus, License string
}

func main() {
	all := flag.Bool("all", false, "build six core targets")
	options := buildOptions{}
	flag.BoolVar(&options.Desktop, "desktop", false, "build native GUI and platform package")
	flag.StringVar(&options.Out, "out", "", "binary output directory")
	flag.StringVar(&options.OS, "os", runtime.GOOS, "target operating system")
	flag.StringVar(&options.Arch, "arch", runtime.GOARCH, "target architecture")
	flag.StringVar(&options.Opus, "opus-library", "", "optional target-platform Opus library to bundle")
	flag.StringVar(&options.License, "opus-license", "", "Opus license notice, required when bundling")
	flag.Parse()
	if *all && options.Desktop {
		fail(fmt.Errorf("桌面版请为每个平台分别构建；--all 用于无 Cgo 核心"))
	}
	if options.Out == "" {
		options.Out = "dist/superflow"
		if options.Desktop {
			options.Out = "dist/superflow-desktop"
		}
	}
	targets := [][2]string{{options.OS, options.Arch}}
	if *all {
		targets = [][2]string{{"darwin", "arm64"}, {"darwin", "amd64"}, {"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}}
	}
	if err := os.MkdirAll(options.Out, 0755); err != nil {
		fail(err)
	}
	for _, target := range targets {
		options.OS, options.Arch = target[0], target[1]
		if err := buildTarget(options); err != nil {
			fail(err)
		}
	}
}
func buildTarget(options buildOptions) error {
	if options.Desktop && options.Opus != "" {
		if options.License == "" {
			return fmt.Errorf("包含 Opus 时请同时提供 --opus-license")
		}
		if err := validateLibrary(options.Opus, options.OS, options.Arch); err != nil {
			return err
		}
	}
	if options.OS != "darwin" && options.OS != "windows" && options.OS != "linux" {
		return fmt.Errorf("不支持的系统：%s", options.OS)
	}
	if options.Arch != "amd64" && options.Arch != "arm64" {
		return fmt.Errorf("不支持的架构：%s", options.Arch)
	}
	if options.Desktop && options.OS == "darwin" && runtime.GOOS != "darwin" {
		return fmt.Errorf("macOS 桌面包需要在 Mac 上构建")
	}
	name := "superflow-" + options.OS + "-" + options.Arch
	if options.OS == "windows" {
		name += ".exe"
	}
	output := filepath.Join(options.Out, name)
	targetPackage := "./cmd/superflow"
	if options.Desktop && options.OS == "windows" {
		staged, cleanup, err := windowsSource(options.Arch)
		if err != nil {
			return err
		}
		defer cleanup()
		targetPackage = staged
	}
	args := []string{"build", "-trimpath"}
	ldflags := "-s -w"
	if options.Desktop {
		args = append(args, "-tags", "desktop")
		if options.OS == "windows" {
			ldflags += " -H=windowsgui"
		}
	}
	args = append(args, "-ldflags="+ldflags, "-o", output, targetPackage)
	command := exec.Command("go", args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GOOS=") && !strings.HasPrefix(entry, "GOARCH=") && !strings.HasPrefix(entry, "CGO_ENABLED=") {
			command.Env = append(command.Env, entry)
		}
	}
	cgo := "0"
	if options.Desktop {
		cgo = "1"
	}
	command.Env = append(command.Env, "CGO_ENABLED="+cgo, "GOOS="+options.OS, "GOARCH="+options.Arch)
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s/%s 构建失败（桌面版需要目标系统的 C 编译器和图形开发库）：%w", options.OS, options.Arch, err)
	}
	fmt.Println(output)
	if options.Desktop && options.OS == "windows" {
		if err := verifyWindowsGUI(output, options.Arch); err != nil {
			return err
		}
	}
	if options.Desktop {
		notices, err := dependencyNotices(command.Env, targetPackage)
		if err != nil {
			return err
		}
		options.notices = notices
		root := filepath.Dir(options.Out)
		if options.OS == "darwin" {
			bundle, err := bundleMacAt(root, output, options.Opus, options.License, options.notices)
			if err != nil {
				return err
			}
			fmt.Println(bundle)
		} else {
			archive, err := bundlePortable(root, output, options)
			if err != nil {
				return err
			}
			fmt.Println(archive)
		}
	}
	return nil
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
