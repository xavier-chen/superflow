# SoundFlow

把局域网中多台 Linux / ARM Linux 的系统声音，汇集到 Mac 当前的输出设备。Mac 自己的声音也能同时播放。

这是可以编译运行的 **0.2.0 首版**：Mac 原生 SwiftUI 窗口与菜单栏，Linux 用户级后台服务，无云账号、无音频中转服务器。

## 开始使用

### 1. Mac

打开 `dist/SoundFlow.app`，建议先放入“应用程序”。选择 Mac 的声音输出设备；SoundFlow 默认跟随系统输出。若 macOS 询问本地网络权限，请允许。

点击 **复制连接密钥**。密钥只在首次安装 Linux 端时使用。

### 2. Linux / ARM Linux

把 `dist/SoundFlow-linux-0.2.0.tar.gz` 复制到 Linux 并解压：

```bash
tar -xzf SoundFlow-linux-0.2.0.tar.gz
cd linux
bash install.sh
```

在提示处粘贴 Mac 的连接密钥（终端不显示输入）。安装脚本安装缺少的依赖、验证 Mac，然后启用用户服务。**请用桌面登录用户运行，不要 sudo 整个脚本。**

之后每次用户登录，发送端自动发现已配对的 Mac，验证成功后建立 SoundFlow 虚拟输出、迁移正在播放的应用并发送声音。Mac 退出、暂停或失联约 3 秒后，Linux 会恢复本地输出；恢复连接后重新接管。Linux 默认不再重复外放。

同一 Mac 密钥可用于多台设备，最多 16 路。每台设备可在 Mac 独立静音和调音量。系统声音采集由用户音频服务管理的默认输出及已迁移的应用；直接占用 ALSA 硬件或应用主动切换到其他输出的声音不在此范围。

### 网络发现受限

设备应在同一局域网。访客 Wi-Fi、AP 隔离、跨 VLAN 和防火墙可能阻止发现。可以指定 Mac 的 IPv4 地址，后续继续自动重连：

```bash
bash install.sh --host 192.168.1.10
# 或已安装后重新配对
~/.local/bin/soundflow pair --host 192.168.1.10
```

Mac 接收 UDP **42790**（发现）和 **42791**（加密音频）。不需要路由器端口转发。容器/虚拟机可能无法透传广播，需要 `--host`。

## 支持范围与当前限制

- Mac：macOS 14+，Apple Silicon；`--universal` 可同时构建 Intel 版本。
- Linux：Python 3.9+，PipeWire-Pulse 或 PulseAudio 用户会话；x86_64、aarch64/arm64、armv7 使用同一份 Python 程序，实际可安装性取决于发行版依赖。
- 安装脚本识别 apt、dnf、pacman；系统登录自启使用 systemd 用户服务。没有 systemd 时可手动运行。
- 只有 ALSA、没有用户音频服务的精简 ARM 系统，需要先安装并启动 PipeWire-Pulse / PulseAudio。麦克风仅在你开启对应同步开关后采集。
- 48 kHz、16-bit、立体声 PCM；每路约 1.6–1.8 Mbit/s（含数据包开销），首版未使用 Opus 压缩。
- 30 / 60 / 120 ms 是接收缓冲目标，**不是端到端延迟**。还包括采集、网络和蓝牙延迟。实际延迟与断线切换需要在目标硬件实测。
- 使用 C11 原子环形缓冲和轻微速率校正控制时钟漂移；15 ms UDP 乱序等待、丢包补零、过量积压丢弃、输出限幅。首版丢包隐藏仍可能听出短暂断音。
- 自动恢复涵盖正常退出、网络失联与 systemd 停止/异常退出。突然断电或手工 SIGKILL 的路由恢复发生在 systemd 的 ExecStopPost、下次启动或 `soundflow restore` 时。
- Mac 安装包是本地临时签名，尚无 Developer ID 签名、公证或自动更新。跨机器分发时可能受 Gatekeeper 限制；正式发布前需完成签名、公证和更多硬件测试。

## 常用操作

```bash
~/.local/bin/soundflow discover
~/.local/bin/soundflow doctor
journalctl --user -u soundflow -f
systemctl --user stop soundflow
~/.local/bin/soundflow restore
systemctl --user start soundflow
```

保留 Linux 本地播放（先停止已运行的服务）：

```bash
~/.local/bin/soundflow run --monitor-only
```

卸载 Linux：`bash linux/uninstall.sh`。恢复本地声音后删除程序，保留配对配置。Mac 直接退出并删除应用；密钥保存在登录钥匙串的 `app.soundflow.receiver` 项目中。

## 构建与验证

需要 Xcode Command Line Tools / Swift 5.9+，Mac 端不依赖第三方 Swift 包。

```bash
swift test
bash scripts/build-macos.sh
# 可选：同时构建 Apple Silicon 与 Intel
bash scripts/build-macos.sh --universal
```

Linux 测试依赖 Python 的 `cryptography` 包：

```bash
PYTHONPATH=linux python3 -m unittest discover -s tests/linux -v
```

跨平台集成测试和结果见 [TESTING.md](TESTING.md)。开发诊断程序 `soundflow-probe` 使用真实接收、解密、抖动缓冲和混音逻辑，将声音录成 WAV，不打开扬声器。

## 实现依据

- [PulseAudio null sink 与 monitor](https://wiki.freedesktop.org/www/Software/PulseAudio/Documentation/User/Modules/)
- [parec 原始 PCM 与延迟参数](https://manpages.debian.org/bookworm/pulseaudio-utils/parec.1.en.html)
- [Apple AVAudioSourceNode](https://developer.apple.com/documentation/avfaudio/avaudiosourcenode)
- [ChaCha20-Poly1305 AEAD](https://cryptography.io/en/latest/hazmat/primitives/aead/)

协议格式与安全边界见 [PROTOCOL.md](PROTOCOL.md)。


## 双向麦克风

在「双向麦克风」中选择已配对设备。两路可以独立开启，默认关闭：

- **Mac → Linux**：选择物理麦克风并开启「发送麦克风」。Linux 会建立 `SoundFlow-Mac-Microphone` 并将它设为默认输入；关闭、失联或退出时恢复此前默认输入。已打开的会议/录音应用如果使用固定设备，请在其设置中选择这个输入。
- **Linux → Mac**：选择 Linux 的物理麦克风与 Mac 的虚拟输入，开启「接收麦克风」。在 Mac 会议/录音应用中选择 `BlackHole 2ch` 作为麦克风。此流不会混入耳机播放，也不自动替换 Mac 的物理默认输入。

Mac 需要单独的虚拟音频驱动才能把远端麦克风提供给其他应用。界面的安装按钮会打开官方 BlackHole 2ch 0.7.1 安装器（已校验 SHA-256 和 Apple Developer ID 签名/公证）。系统安装的许可确认、管理员验证及可能的重启由用户完成。当前 Mac 已安装并识别该驱动。

音频使用不同方向/用途的加密密钥，Mac 麦克风下行使用独立的服务器随机会话，避免与 ACK 重用加密 nonce；已配对设备之间仍使用同一连接密钥。两端都排除 SoundFlow 自己的虚拟输入和输出 monitor，防止数字音频回路。建议使用耳机；使用蓝牙麦克风时播放模式和音质可能变化。

Mac 的麦克风采集使用 AVCaptureSession，并对实际格式动态重采样到 48 kHz，兼容不同采样率与声道数的输入。所有语音只做实时传送，不保存录音。
