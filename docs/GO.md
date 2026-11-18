# SuperFlow Go 开发与使用

新版是 SuperFlow 0.6.0-alpha 托盘应用。所有新增应用代码、系统接口适配、界面、构建工具和测试使用 Go。原生 GUI 使用 Fyne，依赖系统图形工具链；无界面核心以 `CGO_ENABLED=0` 构建六个平台目标。旧版源码只保留供回归对照，新版不启动 Swift、Python 或 Deskflow 进程。

## 当前能做什么

- 一台 hub 主控，多台 node；最多 16 个在线节点。任意支持的平台都能担任主控或节点。
- Go 实现键鼠焦点路由、屏幕网格、边缘切换、USB HID 物理按键映射、鼠标拖动与滚轮。
- `Ctrl+Alt+F12` 切到按名称排序的首台已配置远端，再按一次返回本机；`Ctrl+Alt+Esc` 返回本机。
- 断线、队列溢出、退出时释放远端按键与鼠标按钮；Linux 主控同时释放内核输入抓取。
- 可选文本剪贴板同步，节点之间通过 hub 转发。CLI 默认关闭，GUI 向导显示共享开关；连接时不发送已有剪贴板。
- 系统声音与麦克风独立的双向通道；多来源系统声音混音，s16le 限幅。麦克风采集必须显式配置，hub 发送麦克风还必须指定目标设备。
- 采集进程只在目标在线时启动，目标离线后停止。程序不保存真实音频。
- QUIC/TLS 1.3、证书指纹配对、输入优先于剪贴板调度、鼠标 120 Hz 合并、Opus/PCM、限长音频队列、抖动缓冲与丢包补偿。
- 原生图形工作台：设备在线状态、RTT 和吞吐、每设备系统声音增益、声音路由、键鼠和剪贴板开关，以及可拖动的屏幕网格。
- 托盘开始/暂停、返回本机、添加设备和退出；关闭窗口保留连接。新向导完成后记住自动恢复共享，可在设置关闭；已有用户偏好保留。

这是 Go 重写的功能实现，**不兼容 Deskflow 的网络协议**。支持安装到当前用户的应用目录及可选登录启动；目前尚无系统服务/驱动安装器、图片/文件剪贴板、每设备证书撤销、系统音频自动迁移或桌面 portal 授权流程。

## 构建

需要 Go 1.26+。桌面版在相应操作系统构建，需要 C 编译器与原生图形开发库：macOS 使用 Xcode Command Line Tools；Windows 使用 Fyne 支持的 MinGW 图形工具链；Linux 使用 C 编译器、OpenGL 与 X11 开发库（常见 Debian/Ubuntu 包为 `build-essential libgl1-mesa-dev xorg-dev`）。这些是 Fyne 依赖的构建条件，项目没有新增 C/Swift/Python 应用模块。

构建桌面版：

```console
go run ./cmd/superflow-build --desktop
```

产物在 `dist/superflow-desktop/`；Mac 额外生成本机架构的 `dist/SuperFlow.app`，使用本地临时签名，尚未公证。Windows 生成 `dist/SuperFlow-windows-架构.zip`，含多尺寸图标、版本、普通用户权限与按显示器 DPI 清单，不显示命令行窗口。Linux 生成 `dist/SuperFlow-linux-架构.tar.gz`。打开程序默认进入 GUI。

可以用 `--os windows --arch amd64` 或 `--os linux --arch arm64` 指定桌面目标，但仍需目标系统的 C 编译器和图形开发库；例如 Windows 交叉构建需设置对应的 MinGW `CC`。`--all` 仅用于无 Cgo 核心。Linux 桌面运行需要 OpenGL/X11，Wayland 会话使用 Xwayland。

当前预构建 Mac 包要求 macOS 14+，Windows 包以 Windows 10+ 为目标。Linux x64 包由 Ubuntu 24.04 工具链构建，ELF 符号要求 glibc 2.38+；ARM64 包由 Debian 12 构建，要求 glibc 2.34+。这些是当前发行包的运行库基线，旧系统可使用本机工具链重新构建。

三个平台都可选择同时打包目标架构的 libopus 及其许可证，构建器会检查库的格式和架构。例如 Mac：

```console
go run ./cmd/superflow-build --desktop --opus-library /path/to/libopus.0.dylib --opus-license /path/to/COPYING
```

构建本机无界面核心：

```console
go run ./cmd/superflow-build
```

构建六个版本：

```console
go run ./cmd/superflow-build --all
```

输出在 `dist/superflow/`，例如 `superflow-darwin-arm64`、`superflow-windows-amd64.exe`。以下用 `superflow` 代表对应的可执行文件，首次测试可直接使用其完整路径。

程序不要求安装 Deskflow。Opus 编解码使用运行时动态库：macOS 优先读取应用包内 `Contents/Frameworks/libopus.0.dylib`；Linux 优先读取程序旁 `lib/libopus.so.0`，也支持发行版运行库；Windows 将可信来源的 `opus.dll` 放在程序旁。当前 Mac/Linux 发行包包含对应的库和许可，安装到本机时一起复制。也可以把 `audio.codec` 设为 `pcm`，不加载 Opus。`SUPERFLOW_OPUS_LIBRARY` 可以指定库的绝对路径，兼容旧变量 `SOUNDFLOW_OPUS_LIBRARY`。各桌面包均带实际依赖的第三方许可说明。

## 托盘与图形界面

运行桌面版不带参数，或 `superflow ui --config PATH`。首次只需选择键盘鼠标接在哪台电脑。在该电脑点击“开始使用”，复制连接邀请；另一台点击“连接另一台电脑”，粘贴邀请后配对并连接。设备名称、地址和初始布局自动填写，无需编辑 JSON。高级入口仍可改名、填写手动地址或使用旧 SF2 配对码。

新配置开启 `auto_layout`，只有认证成功的连接才会自动安排屏幕位置并保存，已有位置保留。旧配置保留原行为，在“添加设备”复制新邀请时可开启自动布局。首页按照保存的真实坐标显示全部设备。

局域网发现使用 IPv4 组播 `239.255.42.80:42801`，TTL 为 1，发布名称、平台、ID、端口、证书指纹与运行状态，不发送配对密钥。邀请包含授权材料，应仅交给信任的电脑。邀请没有单次使用限制，仍采用现有组密钥模型。

桌面版重连可根据已配对的证书指纹更新地址，并保留原地址作为回退；邀请中的多个地址按短间隔并行尝试，未选中的连接关闭。发现结果不授予访问权，每个候选仍通过 TLS 指纹验证。网络禁止组播时可直接粘贴邀请；仅支持网络可直接互通的电脑，没有云端中转。

- 工作空间：开始/暂停、在线设备、往返延迟、实时吞吐及点击设备切换键鼠目标。
- 设备：连接和缓冲状态、每来源系统声音 0–200% 增益、静音；增益实时生效并保存，重连后恢复。
- 声音与麦克风：提供收听、发送系统声音与关闭音频的快捷设置；四条独立路由仍可调整，编码和缓冲收进高级设置。快捷设置不会开启麦克风，系统声音快捷设置避免同时回采接收输出。
- 键鼠与剪贴板：开关、快捷键说明和平台权限提示。
- 屏幕布局：拖动屏幕吸附到网格，禁止重叠，点击保存才生效；高级编辑支持精确坐标与 Linux 输入路径。
- 设置：关闭窗口后继续运行、打开应用时自动恢复连接、登录后后台启动、安装到本机、系统检查与键鼠重新检测；配置位置与重新加载在高级配置中。

托盘可显示窗口并提供共享菜单；具体点击方式跟随系统托盘行为。Windows/Linux 使用不透明应用图标，Mac 使用系统模板图标。Linux 托盘需要 StatusNotifierWatcher，支持桌面面板稍后出现和重启后重新注册。没有托盘或会话总线时，关闭窗口仍继续共享，再次打开应用会唤回已有窗口。Windows 会还原最小化窗口并请求前台焦点。Ctrl+Q / Ctrl+, 用于退出/设置，Mac 使用 Cmd。

默认不注册登录启动项。在设置开启“登录后在后台启动”时，先安装到当前用户的应用目录，然后注册用户级启动项；下次登录以 `ui --background --config PATH` 启动，已配置用户不会先闪现主窗口，首次未配置仍显示向导。“打开应用时自动恢复连接”独立控制是否启动共享。应用目录与启动项：

| 系统 | 应用位置 | 登录启动 |
|---|---|---|
| macOS | `~/Applications/SuperFlow.app` | 用户 `Library/LaunchAgents/app.superflow.desktop.plist` |
| Windows | `%LOCALAPPDATA%/SuperFlow/SuperFlow.exe`，开始菜单快捷方式 | HKCU `Software/Microsoft/Windows/CurrentVersion/Run` 中的 SuperFlow |
| Linux | `$XDG_DATA_HOME/superflow/superflow` 及应用菜单入口，默认 `~/.local/share` | `$XDG_CONFIG_HOME/autostart/app.superflow.desktop.desktop`，默认 `~/.config` |

可单独在 GUI 点击“安装到本机”，或使用桌面版 `superflow install --config PATH`。安装和启动项保留指定配置路径，不重建配对；不安装驱动或修改系统权限。相同配置只运行一个桌面进程，本机鉴权 IPC 仅用于唤回窗口，不提供设置或输入控制接口。

GUI 直接调用本地 Go 控制层，配置保存保留设备身份并检查并发修改，自动保留新设备布局、音量等不相关的后台更新；不暴露 HTTP 控制端口。旧版 `dist/SoundFlow.app` 与新版 `dist/SuperFlow.app` 是不同程序，请使用新版。

## 两台机器开始使用

主控 Mac 的示例：

```console
superflow init --name mac --peer linux --peer windows
superflow doctor
superflow run
```

第一条命令生成设备身份、证书和 `SF2-` 配对码。把配对码仅交给信任的节点。节点执行：

```console
superflow pair --name linux --server 192.168.1.10:42800
```

按提示粘贴主控配对码，再执行 `superflow run`。Windows 节点用 `--name windows`。设备名必须与主控 `screens` 中的名称一致才能通过边缘或快捷键选择该设备。

默认配置位于系统用户配置目录下的 `SuperFlow/v2.json`。首次使用默认路径会从 `SoundFlow/v2.json` 迁移 Go 旧配置，保留 ID、密钥和证书，旧文件保留。每条命令都可用 `--config PATH` 指定单独配置。`init` 和 `pair` 拒绝覆盖已有文件，避免误删原设备身份；重配对请先备份旧文件并使用新路径。

防火墙允许主控 UDP **42800**。必须能够直接连接主控；无云端中转。旧版 `SF1-` 密钥与 42790/42791 端口不用于新版。

配对码包含服务器证书指纹和组连接密钥。节点认证后才进入会话；客户端同时验证指纹和证书有效期。持有同一配对码的设备属于同一信任组，不能据此声称拥有每设备独立授权或防冒充身份。配置文件包含私密材料，Unix 创建权限为 0600，不要提交到版本库。

## 键鼠平台要求

| 平台 | Go 接口与运行要求 | 当前边界 |
|---|---|---|
| macOS | CoreGraphics / CFRunLoop，通过 Go FFI 调用；辅助功能权限，主控还需输入监控权限 | 普通桌面会话；安全输入或系统权限边界可能限制捕获 |
| Windows | 低级键鼠 hook、SendInput、物理扫描码 | 普通用户桌面；不能跨 UAC 安全桌面或控制更高完整性应用 |
| Linux X11 | 主控使用明确允许的 evdev 物理设备；注入使用 Go X11 XTEST 客户端 | 主控需设备读取和抓取权限，X11 keycode 使用常见 evdev 映射 |
| Linux Wayland | evdev 采集、uinput 注入，全部由 Go 调用内核接口 | 实验性；需 `/dev/uinput` 权限，主控还需指定物理输入设备。主控本机只支持快捷键进入远端，尚无 portal 无特权方案 |

Linux GUI 自动选择 `by-id` / `by-path` 中的稳定键鼠路径并排除自己的虚拟设备，支持在设置重新检测；X11/Xwayland 下自动读取桌面尺寸。发现不到或权限不足会提示，程序不会自动授予权限。命令行主控或自定义设备可指定：

```json
{
  "input_devices": [
    "/dev/input/by-id/你的键盘-event-kbd",
    "/dev/input/by-id/你的鼠标-event-mouse"
  ],
  "screen_width": 1920,
  "screen_height": 1080
}
```

这是合并进现有完整配置的字段片段。未提供尺寸时尝试读取桌面几何；无法读取时需按实际桌面总尺寸填写。通过当前桌面会话的 ACL 或管理员配置授予上述设备访问权限；程序不自动修改系统权限或要求整个程序以 root 运行。Linux Wayland 路径还需要在目标桌面合成器和实体输入设备上验证。

主控布局 `screens` 例子：

```json
[
  {"name":"linux","x":-1,"y":0},
  {"name":"mac","x":0,"y":0},
  {"name":"windows","x":1,"y":0}
]
```

每台机器的整个桌面视作一个矩形，通过归一化坐标适配分辨率。布局位置须唯一；当前不支持不同屏幕的部分边缘映射或按住鼠标按钮跨设备拖拽。

## 音频配置

先单独枚举本机设备：

```console
superflow audio-devices
```

`capture` 为输入，`playback` 为输出；使用返回的 `id`。这不会录音或改变默认设备。

GUI 可配置下面的常用路由。音频命令都来自同一个 Go 可执行文件，`@self` 自动替换成当前程序路径。配置由参数数组组成，不经 shell，不依赖旧版采集程序。手动编辑后需停止并重新加载配置；GUI 保存会自动重启当前共享。

主控接收各节点的系统声音，并把本机物理麦克风发送给 `linux`：

```json
{
  "codec": "opus",
  "buffer_ms": 40,
  "target": "linux",
  "send_microphone": ["@self", "audio-capture", "--device", "物理麦克风ID"],
  "play_system": ["@self", "audio-play"]
}
```

用这个对象替换完整配置的 `audio` 字段。省略 `send_microphone` 即关闭本机麦克风。

Linux 节点把系统声音发送给主控：

```json
{
  "codec": "opus",
  "buffer_ms": 40,
  "send_system": ["@self", "audio-capture", "--loopback"]
}
```

Linux 使用 Go PulseAudio 客户端采集默认输出的 monitor；同时支持 PulseAudio 和 PipeWire-Pulse。也可用 `--loopback --device 输出设备ID` 指定输出。Windows 同一参数使用 WASAPI loopback。它们不会改变本机默认声音路由，所以本机仍会播放声音。

macOS 系统声音采集须选择虚拟输入（例如已有 BlackHole）：`["@self","audio-capture","--device","BlackHole2ch_UID"]`。把需共享的声音路由到该设备由用户设置。当前没有实现系统音频 tap 和自动路由恢复。

节点将物理麦克风反向发送至主控：

```json
"send_microphone": ["@self", "audio-capture", "--device", "物理麦克风ID"]
```

接收端设置 `play_microphone` 并指向虚拟驱动的输出端，会议软件选择驱动对应的输入端。例如已有 BlackHole 的 Mac：

```json
"play_microphone": ["@self", "audio-play", "--device", "BlackHole2ch_UID"]
```

Windows 需要用户安装虚拟音频线驱动，Linux 需要建立虚拟 sink/source，当前版本不自动安装驱动或创建虚拟输入。不要把系统声音重新采集自其接收输出，也不要把远端麦克风回送为本机物理输入，以免建立数字回路。

所有网络音频统一到 48 kHz、s16le 双声道。Windows 根据实际设备 mix format 重采样；macOS AudioQueue 与 Linux 用户音频服务执行设备格式转换。PCM 每包 5 ms，Opus 每包 10 ms。`buffer_ms` 为 10–200 ms 接收预缓冲目标，默认 40 ms；不代表实际端到端延迟。

音频的 `codec` 还支持 `auto`：新 GUI 向导与快捷配置默认使用此模式。握手交换解码能力，所选目标都支持 Opus 且本机可编码时使用 Opus，否则使用 PCM。混合能力设备加入时重新启动相应采集管线，接收端切换解码器并丢弃迟到的旧格式包；序号跨采集重启递增。旧客户端未声明能力时，新发送端选择 PCM。此机制不等同于自适应码率或时钟漂移校正。

无接收路由时音频管理器不启动 5 ms 混音定时器；鼠标发送器只在有待发送位置及其末帧重复时运行定时器。GUI 隐藏后仅低频更新托盘状态，设备图根据布局、焦点和在线成员变化刷新。

## 文本剪贴板

在需要同步的机器上设置 `"clipboard": true`。三个平台 GUI 共用 Fyne 原生剪贴板，通过 UI 线程调度，Linux GUI 不需要额外的 `wl-copy` 或 `xclip`。无界面 CLI 保留系统适配：macOS 使用系统剪贴板工具，Windows 由 Go 调用 Win32 API，Linux 需要桌面提供 `wl-copy` / `wl-paste`（Wayland）或 `xclip`（X11）。

每 400 ms 检测变化，连接时不发送旧内容。JSON 编码后单条消息上限 1 MiB，过大的文本不发送；目前只支持 UTF-8 文本，图片和文件不传送。

## 网络设计

同一 QUIC 连接分离为输入可靠流、剪贴板可靠流和实时数据报。输入流优先级 0，剪贴板优先级 7；传输均受 QUIC 拥塞控制约束。独立流避免剪贴板的有序交付阻塞键鼠流，不保证拥塞时的固定延迟。

鼠标发送绝对归一化位置，最多 120 Hz，只保留最新位置；停止移动后的最后位置额外重复两次，接收端按焦点代次与序号去重。可靠点击/按键还携带当前指针快照，防止最后一帧鼠标丢失导致点错位置。键盘与鼠标按钮不走丢弃队列，可靠队列满时断开并返回本机。

音频采用独立通道标识和序号，数据报不重传。发送队列最多 8 包，积压时丢旧包；每来源接收窗口最多 64 包。丢失 PCM 补静音，Opus 使用 PLC；来源分开解码，再按系统音频/麦克风分别混音。当前只做接收积压修剪，没有实现完整的自适应码率或采样时钟漂移校正。

## 验证

```console
CGO_ENABLED=0 go test ./...
go test -race ./...
go test -tags 'desktop,ci' ./internal/desktop
CGO_ENABLED=0 go test -tags=integration ./tests/gointegration -v
go vet ./...
go run ./cmd/superflow-build --all
```

竞态检查由 Go 工具链自身要求 Cgo；无界面核心依然采用 `CGO_ENABLED=0` 构建。`desktop,ci` 使用无显示服务器的测试驱动验证界面与交互，不替代各操作系统托盘验证。集成测试用 Go 合成信号，不打开实际麦克风或扬声器。

已验证：本机 macOS API 加载、几何结构 ABI 和音频设备枚举，纯 Go 协议/路由/竞态测试，真实 QUIC 三路合成音频传输，六目标核心和四个桌面目标构建。Linux 原生 GUI、托盘生命周期、无托盘后台恢复、原生剪贴板及用户音频服务的具体结果见 `GO-TESTING.md`。Windows 真实 EXE 已检查资源与清单，原生运行、实体键鼠、各平台权限边界、Wayland 合成器、蓝牙设备格式切换和长时间弱网仍须实体测试。

实现依据：[quic-go streams](https://quic-go.net/docs/quic/streams/)、[QUIC datagrams](https://quic-go.net/docs/quic/datagrams/)、[Apple AudioQueue](https://developer.apple.com/documentation/audiotoolbox/audioqueuenewinput(_:_:_:_:_:_:_:))、[Go Windows Core Audio](https://github.com/moutend/go-wca)、[Go PulseAudio](https://github.com/jfreymuth/pulse)、[Linux uinput](https://docs.kernel.org/input/uinput.html)。
