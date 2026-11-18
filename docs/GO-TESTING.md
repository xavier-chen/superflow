# Go 版验证记录

日期：2026-11-18（Asia/Shanghai）。主机为 macOS arm64，Go 1.26.4。以下结果对应 SuperFlow 0.6.0-alpha；编译通过不代表实体设备测试通过。Linux 图形检查使用隔离的 Debian 12（ARM64）/Ubuntu 24.04（x64）与 Xvfb/Openbox/Mesa 会话，不连接实体键鼠和音频设备。

## 已执行

| 项目 | 范围与结果 |
|---|---|
| 无 Cgo 核心构建 | macOS / Windows / Linux，amd64 / arm64 六目标均成功，产物在 `dist/superflow/` |
| 原生桌面版构建 | macOS arm64、Windows amd64、Linux amd64/arm64 均成功；ZIP/tar.gz 包含图标、说明与实际依赖的第三方许可；Mac/Linux 随包包含 libopus 与许可证；Mac 本地临时签名验证通过，尚未公证 |
| Windows 原生资源 | 检查真实桌面 EXE 的 PE 架构、GUI 子系统、图标、版本、asInvoker、per-monitor V2 DPI 清单通过；amd64/arm64 资源对象生成测试通过；静态导入仅为 Windows 系统 DLL，没有缺失的 MinGW 运行库依赖。没有 Windows 真机或 VM，未声称 Windows 托盘实际运行通过 |
| Linux 原生 GUI | ARM64 与 x64 容器中的真实 Fyne/OpenGL 驱动渲染六页；D-Bus StatusNotifierWatcher 检查实际托盘图标、调用托盘 Activate 唤回、重启 watcher 后重新注册均通过。x64 的默认 llvmpipe 在仿真中返回黑帧，改用 softpipe 后逐页检查正常；检查程序已增加非空帧断言。x64 使用 amd64 仿真，不等同于实体 x64 桌面 |
| Linux 运行库基线 | 检查真实 ELF 的版本需求：x64 最大 GLIBC_2.38，ARM64 最大 GLIBC_2.34。旧发行版需要使用对应本机工具链构建 |
| 后台与二次启动 | Linux ARM64 有托盘、无托盘、后台启动、无会话总线四场景通过；关闭窗口保持 loopback 共享服务，再次启动唤回原窗口。单实例真实子进程与鉴权拒绝、锁释放恢复测试通过 |
| 原生剪贴板 | Linux ARM64 隔离 X11 会话中的中文文本读写通过；共用 Go 适配的长度、UTF-8、取消行为通过。没有读取宿主机剪贴板 |
| Linux 随包 Opus | 解压真实 ARM64 发行包并执行 doctor，动态加载器日志确认从包内 `lib/libopus.so.0` 加载成功；不需要设置库路径。x64 包也包含对应架构的库 |
| 当前用户安装/登录启动 | 临时目录安装可重复、保留自定义配置路径、复制应用和图标、桌面入口转义与换行拒绝、Mac LaunchAgent XML 参数及 plutil 检查通过；未实际更改宿主机应用目录、登录启动项或用户配置 |
| Mac 窗口生命周期 | 0.4 版本实际打开 GUI、关闭隐藏后唤回、通过原生菜单正常退出；0.5 新向导使用测试驱动验证，未开启真实键鼠捕获或麦克风 |
| GUI 渲染与交互 | `desktop,ci` 测试驱动渲染工作空间、设备、音频、键鼠、布局、设置六页；新版浅色界面截图逐页检查，修正重叠和截断；布局吸附、重叠拒绝、未保存内容隔离、保留自定义音频命令、虚拟麦克风检查、开关键盘操作及诊断信息脱敏通过 |
| 配置与控制层 | 并发旧配置迁移保留身份；配置保存冲突拒绝覆盖；设置保存恢复已有运行服务；真实 QUIC 重连后增益恢复；控制层竞态检查通过 |
| 自动连接与布局 | 一份邀请保留指纹和地址；真实 QUIC 验证认证成功才自动加入布局并保存，错误密钥不修改布局；多地址测试先连接失效地址，再通过有效候选完成握手，无需等待第一条超时 |
| 局域网发现 | 同机两个独立浏览器通过真实网卡组播收到彼此的测试公告；纯 UDP 收包、无凭据广播、过期移除及按既有指纹解析地址检查通过；不等同于跨实体机器或所有路由器环境验证 |
| 自动音频编码 | 能力协商覆盖混合能力、旧节点、本机缺库及指定目标；接收端格式切换、迟到旧格式拒绝、PCM 恢复检查通过；自动模式三路 QUIC 合成音频通过 |
| 界面与空闲行为 | 首次欢迎、粘贴邀请、复制邀请页面渲染检查通过；纯指标刷新保持原设备图对象，焦点变化更新设备图；无接收音频时不启动混音定时器，静止鼠标结束末帧重复后停止定时器。尚未给出实体机 CPU/功耗改善百分比 |
| Go 单元测试 | 认证、消息长度边界、配置权限和防覆盖、键鼠焦点/序号/断线释放、点击修复丢失鼠标位置、混音限幅、乱序/丢包/积压、环形缓冲并发、剪贴板首连隐私及回显抑制等通过 |
| 竞态检查 | `go test -race ./...` 在本机通过；未实际开启键鼠捕获或物理麦克风 |
| QUIC 实际连接 | 在本机建立真实加密 UDP 连接，输入可靠流、约 500 KiB 剪贴板、鼠标/音频数据报并行通过；错误 token / 证书指纹被拒绝 |
| 三通道端到端 | Go 合成的 440 Hz 系统音、880 Hz 上行麦克风、660 Hz 下行麦克风，使用完整 App 和独立 Go 采集/播放子进程，通过 PCM、Opus、自动三模式；每路检测到至少 48,000 帧，通道分离校验通过 |
| Opus 实际编解码 | 1 秒 440 Hz 测试音：PCM 192,000 bytes，Opus 12,977 bytes，约 103.8 kbit/s；仅为合成信号载荷，不含包头、QUIC/IP 开销，不代表所有声音的实际码率 |
| macOS 系统接口 | Go FFI 加载 CoreGraphics / CoreFoundation / AudioQueue 成功；读取桌面 CGRect，枚举音频设备成功；未模拟用户键盘或录制真实麦克风 |
| Linux 原生音频接口 | arm64 PulseAudio 容器：纯 Go 播放 → 隔离 null sink → 纯 Go monitor 采集，48,000 帧、RMS 3,535.2；正常退出通过，没有打开物理声卡 |
| 旧版移除回归 | 移除专属耳机姿态功能、校准、对应测试、权限声明和文档；旧版 Swift 剩余 9 项测试通过，通用 Mac 安装包重新构建；原 Python 11 项回归通过 |

## 复现 Go 检查

```console
CGO_ENABLED=0 go test ./...
go test -race ./...
go test -tags 'desktop,ci' ./internal/desktop
SUPERFLOW_DISCOVERY_TEST=1 CGO_ENABLED=0 go test ./internal/discovery -run TestMulticastDiscovery -v -count=1
CGO_ENABLED=0 go test -tags=integration ./tests/gointegration -v
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go vet ./...
go run ./cmd/superflow-build --all
go run ./cmd/superflow-build --desktop
SUPERFLOW_WINDOWS_BINARY="$PWD/dist/superflow-desktop/superflow-windows-amd64.exe" go test ./cmd/superflow-build -run 'TestWindowsResources|TestPackagedWindowsGUI' -v
```

竞态检查使用 Go 自己的竞态运行时。无界面核心支持无 Cgo 构建，Fyne 桌面版需要 Cgo 和系统图形工具链。Opus 库未安装时编解码测试会显示 SKIP，不能记作已验证 Opus。GUI 测试截图位于 `.test-results/superflow-gui/`，使用测试字体参数导出，不将系统字体随应用分发。Linux 原生截图及机器可读报告在 `.test-results/native-linux-arm64/{tray,no-tray,background,no-bus}/` 和 `.test-results/native-linux-amd64/softpipe/`；复现方式见 [桌面检查说明](../tests/desktop/README.md)。

导出 GUI 六页供视觉检查：

```console
SUPERFLOW_TEST_FONT='/path/to/a/CJK-font.ttf' SUPERFLOW_SCREENSHOTS="$PWD/.test-results/superflow-gui" go test -tags 'desktop,ci' ./internal/desktop -run 'TestRender' -count=1
```

Linux 音频测试需要隔离的 PulseAudio 或 PipeWire-Pulse 会话，测试自行建立临时 null sink 并在正常退出时移除：

```console
SUPERFLOW_NATIVE_AUDIO_TEST=1 go test ./internal/audio -run TestNativePulseLoopback -v
```

从 Mac 使用已有测试容器复现（容器中的声音设备是合成的）：

```console
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o .build/audio-linux-arm64.test ./internal/audio
docker run --rm -v "$PWD:/work:ro" --entrypoint /bin/sh soundflow-test:arm64 -c 'mkdir -p /tmp/superflow-go-runtime && chmod 700 /tmp/superflow-go-runtime && export XDG_RUNTIME_DIR=/tmp/superflow-go-runtime && pulseaudio --start --exit-idle-time=-1 && SUPERFLOW_NATIVE_AUDIO_TEST=1 /work/.build/audio-linux-arm64.test -test.run TestNativePulseLoopback -test.v -test.timeout 20s'
```

## 尚需验证

- 实体 Windows 与 Linux 键鼠捕获/注入、布局、组合键和不同桌面权限。
- Wayland evdev/uinput 路径的设备分类、坐标、多显示器和合成器行为；目前不能视为已完成桌面兼容验证。
- macOS 实际事件 tap 与 AudioQueue 输入的权限、麦克风格式切换及断开恢复。
- 三台实体设备同时运行、Wi-Fi 拥塞/丢包、睡眠唤醒、长时间时钟漂移和端到端听感。
- Windows 原生托盘、系统字体、多屏 DPI、最小化恢复、开始菜单与登录启动的真机验证；Linux 不同桌面面板、系统字体、多屏缩放和睡眠唤醒的实体验证。
- 真实用户会话的登录启动（Windows Run、Mac LaunchAgent、Linux autostart）；目前只验证文件/参数生成及 Linux 后台启动流程。
- 虚拟输入设备安装、系统声音自动迁移、正式签名与公证、系统级驱动/服务安装器。

以上未验证项目不包含在“六平台构建成功”的结论中。
