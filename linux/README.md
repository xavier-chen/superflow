# SoundFlow Linux 发送端

先打开 Mac 上的 SoundFlow，点击“复制连接密钥”。在 Linux 解压安装包后，进入此目录：

```bash
bash install.sh
```

用桌面登录用户运行，不要 sudo 整个脚本。首次安装会提示粘贴密钥，之后自动发现并连接同一 Mac。支持 x86_64、arm64/aarch64、armv7；需要 Python 3.9+、cryptography 和 PipeWire-Pulse / PulseAudio。

自动发现受限时：`bash install.sh --host MAC的IPv4地址`。

安装后正常情况下无需操作。管理命令：

```bash
~/.local/bin/soundflow doctor
journalctl --user -u soundflow -f
systemctl --user stop soundflow
systemctl --user start soundflow
```

Mac 暂停、退出或断开网络后，Linux 自动恢复本地声音。没有 systemd 用户服务时，手动运行 `~/.local/bin/soundflow run`。意外关闭后可用 `~/.local/bin/soundflow restore` 恢复声音。

卸载：`bash uninstall.sh`。卸载保留配对配置。如需更换 Mac，请重新运行 `~/.local/bin/soundflow pair` 并重启服务。

0.2.0 支持双向麦克风，在 Mac 的「双向麦克风」页分别开启：

- Mac → Linux：建立 `SoundFlow-Mac-Microphone` 作为默认输入，关闭或断线后恢复。
- Linux → Mac：仅采集已接入的物理麦克风；Mac 应用选择 BlackHole 2ch 虚拟输入。

两路使用独立加密通道，排除输出监听与 SoundFlow 自己的虚拟输入。没有接入麦克风时，Mac 会显示该状态，不使用系统声音冒充麦克风。

精简 ARM 系统如果只有 ALSA，需要先建立运行中的用户音频服务。防火墙需允许向 Mac 的 UDP 42790、42791 发送并接收回复。
