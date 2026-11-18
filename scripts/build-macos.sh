#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_dir"
arch_flags=(--arch "$(uname -m)")
if [[ "${1:-}" == --universal ]]; then
  arch_flags=(--arch arm64 --arch x86_64)
fi
swift build -c release "${arch_flags[@]}" --product SoundFlow
build_dir="$(swift build -c release "${arch_flags[@]}" --show-bin-path)"
app_dir="$project_dir/dist/SoundFlow.app"
mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources"
cp "$build_dir/SoundFlow" "$app_dir/Contents/MacOS/SoundFlow"
if [[ -f "$project_dir/dist/Extras/BlackHole2ch-0.7.1.pkg" ]]; then
  cp "$project_dir/dist/Extras/BlackHole2ch-0.7.1.pkg" "$app_dir/Contents/Resources/"
fi
cat > "$app_dir/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>app.soundflow.receiver</string>
<key>CFBundleName</key><string>SoundFlow</string>
<key>CFBundleDisplayName</key><string>SoundFlow</string>
<key>CFBundleExecutable</key><string>SoundFlow</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.2.0</string>
<key>CFBundleVersion</key><string>2</string>
<key>CFBundleIconFile</key><string>SoundFlow</string>
<key>LSMinimumSystemVersion</key><string>14.0</string>
<key>LSUIElement</key><true/>
<key>NSHighResolutionCapable</key><true/>
<key>NSLocalNetworkUsageDescription</key><string>SoundFlow 需要发现局域网内的 Linux 设备并接收其声音，汇集到你的耳机播放。</string>
<key>NSMicrophoneUsageDescription</key><string>SoundFlow 在你开启麦克风同步时采集所选麦克风，并加密传送给已配对的 Linux 设备。</string>
</dict></plist>
PLIST
icon_dir="$project_dir/.build/SoundFlow.iconset"
mkdir -p "$icon_dir"
swift scripts/make-icon.swift "$icon_dir/icon_512x512@2x.png"
for pixels in 16 32 128 256 512; do
  sips -z "$pixels" "$pixels" "$icon_dir/icon_512x512@2x.png" --out "$icon_dir/icon_${pixels}x${pixels}.png" >/dev/null
  if [[ "$pixels" != 512 ]]; then
    twice=$((pixels * 2))
    sips -z "$twice" "$twice" "$icon_dir/icon_512x512@2x.png" --out "$icon_dir/icon_${pixels}x${pixels}@2x.png" >/dev/null
  fi
done
iconutil -c icns "$icon_dir" -o "$app_dir/Contents/Resources/SoundFlow.icns"
codesign --force --deep --sign - "$app_dir"
ditto -c -k --sequesterRsrc --keepParent "$app_dir" "$project_dir/dist/SoundFlow-macOS-0.2.0.zip"
COPYFILE_DISABLE=1 tar --no-xattrs --exclude=__pycache__ -czf "$project_dir/dist/SoundFlow-linux-0.2.0.tar.gz" linux
echo "已生成 ${app_dir}，以及 Mac / Linux 安装归档。"
