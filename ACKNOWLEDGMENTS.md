# Open-source acknowledgments

Thank you to the maintainers and contributors who make SuperFlow's desktop, networking, and audio features possible.

| Project | Contribution |
| --- | --- |
| [Fyne](https://github.com/fyne-io/fyne) and [fyne.io/systray](https://github.com/fyne-io/systray) | Shared desktop UI and native tray integration |
| [quic-go](https://github.com/quic-go/quic-go) | QUIC streams and datagrams for input, clipboard, and audio |
| [Opus](https://opus-codec.org/) | Audio compression and packet loss concealment |
| [purego](https://github.com/ebitengine/purego) and [go-wca](https://github.com/moutend/go-wca) | Native system calls and Windows Core Audio integration |
| [Go PulseAudio client](https://github.com/jfreymuth/pulse), [XGB](https://github.com/jezek/xgb), and [godbus](https://github.com/godbus/dbus) | Linux audio, X11 input, and D-Bus integration |
| [winres](https://github.com/tc-hib/winres) | Windows executable icons, manifests, and version resources |
| [Go fonts](https://go.dev/blog/go-fonts) by Bigelow & Holmes | Typeface used for the documentation wordmark |

[Deskflow](https://github.com/deskflow/deskflow) inspired the multi-computer keyboard and mouse workflow. The modules used by SuperFlow are listed in [go.mod](go.mod). Desktop builds include third-party license texts in `THIRD_PARTY_NOTICES.txt` and the Opus license when its library ships with the package. The wordmark's font notice is included with the [brand assets](docs/assets/GO-FONT-LICENSE.txt).

## 开源致谢

感谢维护者和贡献者。Fyne 与 systray 提供界面和托盘，quic-go 提供网络传输，Opus 提供音频压缩与丢包补偿。purego、go-wca、Go PulseAudio 客户端、XGB 和 godbus 支撑各平台系统接口，winres 用于生成 Windows 应用资源。文档字标采用 Bigelow & Holmes 为 Go 设计的字体。

感谢 Deskflow 对多电脑键鼠工作流的启发。实际依赖见 [go.mod](go.mod)，发行包附带第三方许可说明。
