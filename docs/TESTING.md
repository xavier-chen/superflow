# 验证记录与复现

验证日期：2026-11-15（Asia/Shanghai）。Mac 为 Apple Silicon，Swift 6.3.3。Linux 测试在 OrbStack 容器中执行；arm64 原生运行，x86_64 通过本机容器兼容层运行。这些结果不代表已经在实体 ARM 开发板或 Intel Mac 上完成测试。

## 已通过

| 项目 | 结果与实际验证范围 |
|---|---|
| Swift 单元测试 | 8 项通过：报文加密/篡改/错误密钥、连接码、重放窗口、乱序与丢包、长时间戳跳变、多路混音/限幅/静音、预缓冲/欠载、150 秒模拟时钟漂移 |
| Thread Sanitizer | 同一组 8 项单元测试通过；不等同于完整硬件链路的并发审计 |
| Linux 单元测试 | 7 项，在 arm64 与 x86_64 容器通过：协议、文件权限、路由恢复、用户主动换输出、模块 ID 重用保护、无声卡 dummy sink 恢复、失败时保留恢复文件 |
| 多路自动发现与混音 | 两个独立 Python 发送进程通过组播/广播发现 Swift 接收端，生成不同频率测试音，接收端混音为 WAV |
| ARM Linux 真实采集 | aarch64：启动 PulseAudio，建立本地虚拟声卡，自动迁移默认输出，`paplay → parec → 加密 UDP → Swift → WAV`，停止后验证恢复原输出并移除 SoundFlow 虚拟声卡 |
| x86_64 Linux 真实采集 | 相同流程通过，接收端识别为 x86_64；WAV 包含实际采集的声音 |
| PipeWire-Pulse | aarch64：PipeWire 0.3.65 + WirePlumber 0.4，执行相同真实采集与恢复流程通过 |
| 网络断开与恢复 | 接收端自然关闭后，Linux 自动恢复本地输出；用同一接收端身份重新启动后自动发现并接管输出；最终 SIGTERM 再次恢复本地输出 |
| Linux 安装流程 | 在隔离的普通用户 HOME 下运行真实安装脚本，交互式配对并验证连接密钥、检查 0600 配置权限、运行 doctor、卸载并保留配置 |
| Mac 界面 | 原生窗口启动成功，识别当前系统输出，暂停/继续按钮状态正确；程序没有打开麦克风 |
| Mac 安装包 | arm64 + x86_64 通用二进制构建成功，本地代码签名完整性检查通过 |

网络链路测试录音中的 1 次欠载发生在发送端结束后接收端继续录制的阶段。测试没有观察到丢包；人工注入的丢包与乱序由单元测试覆盖，尚未在拥塞 Wi-Fi 上测量听感。

## 复现命令

在 Mac 项目目录执行。Python 命令需要装有 `cryptography` 的虚拟环境：

```bash
python3 -m venv .venv
.venv/bin/python -m pip install cryptography
swift test
swift test --sanitize=thread
# 还原普通诊断程序，以便运行后续网络测试
swift build
PYTHONPATH=linux .venv/bin/python -m unittest discover -s tests/linux -v
.venv/bin/python tests/integration.py
```

真实 Linux 捕获、安装和重连测试：

```bash
docker build -f tests/Dockerfile -t soundflow-test:arm64 .
docker run --rm -v "$PWD:/work:ro" soundflow-test:arm64
.venv/bin/python tests/integration.py --docker-image soundflow-test:arm64
.venv/bin/python tests/integration.py --docker-image soundflow-test:arm64 --install
.venv/bin/python tests/reconnect.py soundflow-test:arm64

docker build --platform linux/amd64 -f tests/Dockerfile -t soundflow-test:amd64 .
.venv/bin/python tests/integration.py --docker-image soundflow-test:amd64

docker build -f tests/Dockerfile.pipewire -t soundflow-test:pipewire .
.venv/bin/python tests/integration.py --docker-image soundflow-test:pipewire --pipewire
```

本机下载 Debian amd64 基础镜像时遇到仓库连接超时，实际 x86_64 验证改用本机已有的 Ubuntu 系镜像 `sharelatex/sharelatex:5.5.4` 作为基础层，覆盖容器入口，仅安装并运行测试依赖。复现该环境可在构建时追加 `--build-arg BASE_IMAGE=sharelatex/sharelatex:5.5.4`。

PipeWire 测试容器没有 systemd-logind，因此仅在测试镜像中关闭 WirePlumber 0.4 的 Bluetooth seat monitor，保留音频路由策略。该配置不安装到用户机器；配置语法依据 [WirePlumber 官方迁移文档](https://pipewire.pages.freedesktop.org/wireplumber/daemon/configuration/migration.html)。

测试输出保存在 `.test-results/`（已加入 gitignore），包括 WAV、各平台 JSON 报告及重连检查点。诊断端口为 42890/42891、42990/42991，避免干扰已打开的 Mac 应用。测试密钥为公开的固定 fixture，与 Mac 应用钥匙串中的随机密钥无关。

## 仍需实体设备验证

- 两台实体 Linux / ARM 电脑与 Mac 的局域网发现，及多网卡/访客网/不同防火墙配置。
- 实际听感和端到端延迟、休眠/断连/重新连接后的音频恢复。
- 长时间混音、拥塞 Wi-Fi、不同采样率的实体输出设备。
- 实体 Intel Mac、ARMv7、其他发行版及 systemd 登录自启。
- Developer ID 签名、公证与正式分发。

## 0.2.0 追加验证（2026-11-15）

- Swift 音频与协议测试包含系统音频/ACK/双向麦克风密钥隔离，当前保留 9 项测试。
- Python 11 项测试通过，新增麦克风重放/服务器会话更换、排除系统输出 monitor 和虚拟输入、排除全部插孔都报告未接入的源。
- arm64 容器的双向麦克风合成信号测试通过：Linux 523.25 Hz 输入到 Swift 的独立麦克风混音器；Swift 660 Hz 输入到 Linux 真正的虚拟麦克风，再采集并验证频率与 RMS；关闭后输入和输出都恢复。
- 最新两发送端系统音频回归通过，440 Hz 与 660 Hz 两个来源均在混音结果中检出。
- BlackHole 2ch 0.7.1 已由用户完成系统安装并被 Mac 识别，官方安装包 SHA-256、Apple Developer ID Installer 签名与公证状态已核验。
- 实体物理麦克风 → Mac → ThinkCentre 的虚拟输入已测到 48,000 帧真实信号，RMS 19.72、峰值 173（s16le 标度），没有保存语音。
- 真实耳机测试发现 AVAudioEngine 显式 tap 在切换 48 kHz/24 kHz 输入时会发生格式异常；改为 AVCaptureSession 的实际 sample-buffer 格式动态转换后，持续运行数分钟无采集错误，并同时保持系统声音和反向麦克风通道。
- ThinkCentre 的物理输入前、后插孔均报告未接入，当前软件会显示无可用物理麦克风；反向真实语音需接入麦克风后进一步验证。

后续尝试再次运行容器回归时，OrbStack/Docker daemon 未运行；本节的容器双向结果来自 daemon 运行时已完成的测试。最终 Mac 采集格式修正另以实体输入设备与 ThinkCentre 验证，最新本地系统音频回归及全部单元测试已通过。

复现双向合成信号：

```bash
swift build
.venv/bin/python tests/integration.py --docker-image soundflow-test:arm64 --duplex
```

开发参数 `SoundFlow --verify-microphones` 会在同一应用中开启两路麦克风并将连接状态、信号电平和包计数写入用户缓存下的 0600 元数据文件，不记录语音或密钥。正常启动不启用该模式，麦克风开关默认关闭。
