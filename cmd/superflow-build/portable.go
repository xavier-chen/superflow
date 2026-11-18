package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"superflow.local/superflow/internal/brand"
	"superflow.local/superflow/internal/version"
)

func bundlePortable(root, executable string, options buildOptions) (string, error) {
	stage, err := os.MkdirTemp(root, ".superflow-package-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	name := "superflow"
	if options.OS == "windows" {
		name = "SuperFlow.exe"
	}
	if err = copyFile(executable, filepath.Join(stage, name), 0755); err != nil {
		return "", err
	}
	png, err := brand.PNG(256)
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(stage, "SuperFlow.png"), png, 0644); err != nil {
		return "", err
	}
	if len(options.notices) > 0 {
		if err = os.WriteFile(filepath.Join(stage, "THIRD_PARTY_NOTICES.txt"), options.notices, 0644); err != nil {
			return "", err
		}
	}
	if options.OS == "windows" {
		ico, e := brand.ICO()
		if e != nil {
			return "", e
		}
		if e = os.WriteFile(filepath.Join(stage, "SuperFlow.ico"), ico, 0644); e != nil {
			return "", e
		}
	}
	if options.Opus != "" {
		if options.License == "" {
			return "", fmt.Errorf("包含 Opus 时请同时提供 --opus-license")
		}
		if err = validateLibrary(options.Opus, options.OS, options.Arch); err != nil {
			return "", err
		}
		library := "opus.dll"
		if options.OS == "linux" {
			library = filepath.Join("lib", "libopus.so.0")
			if err = os.MkdirAll(filepath.Join(stage, "lib"), 0755); err != nil {
				return "", err
			}
		}
		if err = copyFile(options.Opus, filepath.Join(stage, library), 0755); err != nil {
			return "", err
		}
		if err = copyFile(options.License, filepath.Join(stage, "Opus-LICENSE.txt"), 0644); err != nil {
			return "", err
		}
	}
	instructions := fmt.Sprintf("SuperFlow %s (%s/%s)\n\n", version.Release, options.OS, options.Arch) +
		"打开 SuperFlow，在键盘鼠标所在电脑复制连接邀请，在另一台粘贴连接。\n无需分别填写 IP、设备名称或修改配置文件。\n\n" +
		"关闭窗口后继续共享；再次打开 SuperFlow 将返回已有窗口。\n可在设置中安装到当前用户的应用菜单，并选择登录后在后台启动。\n系统权限需要按提示授权。\n\n" +
		"Windows: 双击 SuperFlow.exe，无需以管理员身份运行。\nLinux: 运行 ./superflow；需要正常桌面会话及系统 OpenGL/X11 运行库。\nLinux 没有托盘服务时仍可通过应用菜单再次打开。\n\n" +
		"声音快捷设置与自动编码在三平台一致。缺少 Opus 时自动模式使用 PCM。\n本版本为 alpha，实体键鼠、Wayland 权限及不同桌面环境仍需验证。\n"
	if err = os.WriteFile(filepath.Join(stage, "README.txt"), []byte(instructions), 0644); err != nil {
		return "", err
	}
	prefix := "SuperFlow-" + options.OS + "-" + options.Arch
	suffix := ".tar.gz"
	if options.OS == "windows" {
		suffix = ".zip"
	}
	output := filepath.Join(root, prefix+suffix)
	temp, err := os.CreateTemp(root, ".superflow-archive-*")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if options.OS == "windows" {
		err = writeZIP(temp, stage, prefix)
	} else {
		err = writeTar(temp, stage, prefix)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = os.Rename(tempName, output); err != nil {
		return "", err
	}
	return output, nil
}
func writeZIP(out io.Writer, root, prefix string) error {
	writer := zip.NewWriter(out)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		header.Name = prefix + "/" + filepath.ToSlash(relative)
		header.Method = zip.Deflate
		target, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		_, err = io.Copy(target, source)
		return err
	})
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return err
}
func writeTar(out io.Writer, root, prefix string) error {
	compressed := gzip.NewWriter(out)
	writer := tar.NewWriter(compressed)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		header.Name = prefix + "/" + filepath.ToSlash(relative)
		if err = writer.WriteHeader(header); err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		_, err = io.Copy(writer, source)
		return err
	})
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if closeErr := compressed.Close(); err == nil {
		err = closeErr
	}
	return err
}
func validateLibrary(path, platform, arch string) error {
	valid := false
	switch platform {
	case "linux":
		file, err := elf.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		valid = arch == "amd64" && file.Machine == elf.EM_X86_64 || arch == "arm64" && file.Machine == elf.EM_AARCH64
	case "windows":
		file, err := pe.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		valid = arch == "amd64" && file.Machine == pe.IMAGE_FILE_MACHINE_AMD64 || arch == "arm64" && file.Machine == pe.IMAGE_FILE_MACHINE_ARM64
	case "darwin":
		file, err := macho.Open(path)
		if err == nil {
			defer file.Close()
			valid = arch == "amd64" && file.Cpu == macho.CpuAmd64 || arch == "arm64" && file.Cpu == macho.CpuArm64
		} else {
			fat, e := macho.OpenFat(path)
			if e != nil {
				return err
			}
			defer fat.Close()
			for _, part := range fat.Arches {
				if arch == "amd64" && part.Cpu == macho.CpuAmd64 || arch == "arm64" && part.Cpu == macho.CpuArm64 {
					valid = true
				}
			}
		}
	}
	if !valid {
		return fmt.Errorf("Opus 库与 %s/%s 不匹配", platform, arch)
	}
	return nil
}
