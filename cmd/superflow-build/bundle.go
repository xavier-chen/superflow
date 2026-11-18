package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"superflow.local/superflow/internal/brand"
	"superflow.local/superflow/internal/version"
)

func bundleMac(executable, opusLibrary, opusLicense string) (string, error) {
	return bundleMacAt("dist", executable, opusLibrary, opusLicense)
}
func bundleMacAt(root, executable, opusLibrary, opusLicense string, notices ...[]byte) (string, error) {
	bundle := filepath.Join(root, "SuperFlow.app")
	contents := filepath.Join(bundle, "Contents")
	macos := filepath.Join(contents, "MacOS")
	resources := filepath.Join(contents, "Resources")
	for _, dir := range []string{macos, resources} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	if err := copyFile(executable, filepath.Join(macos, "SuperFlow"), 0755); err != nil {
		return "", err
	}
	icon, err := brand.PNG(1024)
	if err != nil {
		return "", err
	}
	icns := make([]byte, 16)
	copy(icns, "icns")
	binary.BigEndian.PutUint32(icns[4:8], uint32(16+len(icon)))
	copy(icns[8:12], "ic10")
	binary.BigEndian.PutUint32(icns[12:16], uint32(8+len(icon)))
	icns = append(icns, icon...)
	if err = os.WriteFile(filepath.Join(resources, "SuperFlow.icns"), icns, 0644); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(resources, "SuperFlow.png"), icon, 0644); err != nil {
		return "", err
	}
	if len(notices) > 0 {
		if err = os.WriteFile(filepath.Join(resources, "THIRD_PARTY_NOTICES.txt"), notices[0], 0644); err != nil {
			return "", err
		}
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>app.superflow.desktop</string>
<key>CFBundleName</key><string>SuperFlow</string>
<key>CFBundleDisplayName</key><string>SuperFlow</string>
<key>CFBundleExecutable</key><string>SuperFlow</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>%s</string>
<key>CFBundleVersion</key><string>%d</string>
<key>CFBundleIconFile</key><string>SuperFlow</string>
<key>LSMinimumSystemVersion</key><string>14.0</string>
<key>LSUIElement</key><true/>
<key>NSHighResolutionCapable</key><true/>
<key>NSLocalNetworkUsageDescription</key><string>SuperFlow 与已配对的局域网设备共享声音、键鼠与剪贴板。</string>
<key>NSMicrophoneUsageDescription</key><string>仅在你开启麦克风共享后，SuperFlow 才采集选择的麦克风并发送给目标设备。</string>
</dict></plist>
`, version.Number, version.Build)
	if err = os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0644); err != nil {
		return "", err
	}
	if opusLibrary != "" {
		if opusLicense == "" {
			return "", fmt.Errorf("包含 Opus 时请同时提供 --opus-license")
		}
		dir := filepath.Join(contents, "Frameworks")
		if err = os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
		if err = copyFile(opusLibrary, filepath.Join(dir, "libopus.0.dylib"), 0755); err != nil {
			return "", err
		}
		if err = copyFile(opusLicense, filepath.Join(resources, "Opus-LICENSE.txt"), 0644); err != nil {
			return "", err
		}
	}
	cmd := exec.Command("codesign", "--force", "--deep", "--sign", "-", bundle)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return "", err
	}
	return bundle, nil
}
func copyFile(source, destination string, mode os.FileMode) error {
	b, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, b, mode)
}
