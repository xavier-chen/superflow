import SwiftUI
import SoundFlowCore

private enum Palette {
    static let canvas = Color(red: 0.965, green: 0.965, blue: 0.975)
    static let text = Color(red: 0.114, green: 0.114, blue: 0.122)
    static let secondary = Color(red: 0.43, green: 0.43, blue: 0.46)
    static let line = Color.black.opacity(0.075)
    static let accent = Color(red: 0.0, green: 0.478, blue: 1.0)
    static let online = Color(red: 0.20, green: 0.66, blue: 0.34)
}

struct Dashboard: View {
    @ObservedObject var model: AppModel
    @State private var tab = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            header
            output
            if let error = model.error {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(.system(size: 12)).foregroundStyle(.orange)
                    .textSelection(.enabled)
            }
            Picker("功能", selection: $tab) {
                Text("声音汇集").tag(0)
                Text("双向麦克风").tag(1)
            }.pickerStyle(.segmented).labelsHidden()
            Group {
                switch tab {
                case 1: MicrophonePanel(model: model)
                default:
                    VStack(spacing: 20) { sources; pairing }
                }
            }.frame(minHeight: 330, maxHeight: .infinity)
            settings
        }
        .padding(28)
        .frame(minWidth: 840, minHeight: 720)
        .foregroundStyle(Palette.text)
        .tint(Palette.accent)
        .background(Palette.canvas)
    }

    private var header: some View {
        HStack(spacing: 12) {
            Image(systemName: "waveform")
                .font(.system(size: 22, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: 44, height: 44)
                .background(Palette.accent, in: RoundedRectangle(cornerRadius: 12))
            VStack(alignment: .leading, spacing: 3) {
                Text("SoundFlow").font(.system(size: 22, weight: .semibold))
                Text("多台设备，一副耳机").font(.system(size: 12)).foregroundStyle(Palette.secondary)
            }
            Spacer()
            HStack(spacing: 6) {
                Circle().fill(model.receiving ? Palette.online : Palette.secondary)
                    .frame(width: 6, height: 6)
                Text(model.receiving ? "正在接收" : "已暂停")
                    .font(.system(size: 11, weight: .medium))
            }
            .foregroundStyle(Palette.secondary)
            .padding(.horizontal, 12).padding(.vertical, 7)
            .background(.white.opacity(0.8), in: Capsule())
            .overlay(Capsule().strokeBorder(Palette.line, lineWidth: 1))
        }
    }

    private var output: some View {
        HStack(spacing: 18) {
            Image(systemName: "headphones")
                .font(.system(size: 34, weight: .regular))
                .foregroundStyle(Palette.text)
                .frame(width: 66, height: 66)
                .background(Palette.canvas, in: RoundedRectangle(cornerRadius: 16))
            VStack(alignment: .leading, spacing: 5) {
                Text("当前输出").font(.system(size: 11, weight: .medium)).foregroundStyle(Palette.secondary)
                Text(model.outputName).font(.system(size: 21, weight: .semibold)).lineLimit(1)
                Text("与 Mac 本机声音一起播放").font(.system(size: 12)).foregroundStyle(Palette.secondary)
            }
            Spacer(minLength: 12)
            Button(action: model.openSoundSettings) {
                HStack(spacing: 6) {
                    Text("更改输出")
                    Image(systemName: "chevron.right").font(.system(size: 9, weight: .semibold))
                }
            }
            .buttonStyle(QuietButtonStyle())
            .help("打开 Mac 声音设置，选择输出设备")
        }
        .padding(20)
        .modifier(WhitePanel())
    }

    private var sources: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text("设备声音").font(.system(size: 14, weight: .semibold))
                Text("\(model.onlineCount)")
                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(Palette.secondary)
                    .padding(.horizontal, 7).padding(.vertical, 3)
                    .background(Palette.canvas, in: Capsule())
                Spacer()
                Button(action: model.toggle) {
                    Label(model.receiving ? "暂停" : "继续接收", systemImage: model.receiving ? "pause.fill" : "play.fill")
                        .font(.system(size: 11, weight: .medium))
                }
                .buttonStyle(QuietButtonStyle())
            }
            .padding(.horizontal, 20).padding(.vertical, 15)

            Rectangle().fill(Palette.line).frame(height: 1)

            if model.peers.isEmpty {
                VStack(spacing: 11) {
                    HStack(spacing: 15) {
                        Image(systemName: "desktopcomputer").font(.system(size: 23, weight: .light))
                        Image(systemName: "ellipsis").font(.system(size: 13, weight: .medium)).foregroundStyle(Palette.secondary.opacity(0.4))
                        Image(systemName: "headphones").font(.system(size: 24, weight: .light))
                    }
                    .foregroundStyle(Palette.secondary.opacity(0.65))
                    .padding(.bottom, 4)
                    Text(model.receiving ? "还没有连接设备" : "声音接收已暂停")
                        .font(.system(size: 14, weight: .medium))
                    Text(model.receiving ? "添加你的 Linux 设备，声音会自动汇集到这里。" : "继续接收后，已配对的设备会自动连接。")
                        .font(.system(size: 12)).foregroundStyle(Palette.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding(.vertical, 28)
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(model.peers) { peer in
                            PeerRow(peer: peer, gain: Binding(
                                get: { model.gain(for: peer.id) },
                                set: { model.setGain($0, id: peer.id) }))
                            if peer.id != model.peers.last?.id {
                                Rectangle().fill(Palette.line).frame(height: 1).padding(.leading, 74)
                            }
                        }
                    }
                }
            }
        }
        .frame(minHeight: 210, maxHeight: .infinity)
        .modifier(WhitePanel())
    }

    private var pairing: some View {
        HStack(alignment: .center, spacing: 14) {
            Image(systemName: "plus.circle")
                .font(.system(size: 23, weight: .regular))
                .foregroundStyle(Palette.accent)
            VStack(alignment: .leading, spacing: 7) {
                Text("添加 Linux 设备").font(.system(size: 13, weight: .semibold))
                Text("在 Linux 上运行安装脚本，粘贴密钥。配对一次，以后自动连接。")
                    .font(.system(size: 11)).foregroundStyle(Palette.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 6) {
                    Image(systemName: "terminal").font(.system(size: 10))
                    Text("bash install.sh").font(.system(size: 11, design: .monospaced)).textSelection(.enabled)
                }
                .foregroundStyle(Palette.secondary)
            }
            Spacer(minLength: 12)
            Button(action: model.copyCode) {
                Label(model.copied ? "已复制" : "复制连接密钥", systemImage: model.copied ? "checkmark" : "doc.on.doc")
                    .font(.system(size: 12, weight: .medium))
            }
            .buttonStyle(PrimaryButtonStyle())
        }
        .padding(20)
        .modifier(WhitePanel())
    }

    private var settings: some View {
        HStack(spacing: 18) {
            VStack(alignment: .leading, spacing: 7) {
                Text("汇入音量").font(.system(size: 10)).foregroundStyle(Palette.secondary)
                HStack(spacing: 8) {
                    Image(systemName: model.master > 0 ? "speaker.wave.2" : "speaker.slash")
                        .font(.system(size: 11)).foregroundStyle(Palette.secondary).frame(width: 16)
                    Slider(value: $model.master, in: 0...1).controlSize(.small).frame(width: 120)
                        .accessibilityLabel("汇入声音总音量")
                    Text("\(Int(model.master * 100))%")
                        .font(.system(size: 10, design: .monospaced)).foregroundStyle(Palette.secondary).frame(width: 32)
                }
            }
            Spacer(minLength: 0)
            VStack(alignment: .leading, spacing: 7) {
                Text("播放缓冲").font(.system(size: 10)).foregroundStyle(Palette.secondary)
                Picker("播放缓冲", selection: $model.buffer) {
                    Text("低延迟 · 30 ms").tag(30)
                    Text("均衡 · 60 ms").tag(60)
                    Text("稳定 · 120 ms").tag(120)
                }
                .labelsHidden().controlSize(.small).frame(width: 150)
            }
            Rectangle().fill(Palette.line).frame(width: 1, height: 30).padding(.horizontal, 4)
            Toggle("登录时启动", isOn: Binding(get: { model.launchAtLogin }, set: model.setLogin))
                .toggleStyle(.checkbox).font(.system(size: 11)).controlSize(.small)
                .foregroundStyle(Palette.secondary)
        }
        .padding(.horizontal, 20).padding(.vertical, 15)
        .modifier(WhitePanel())
    }
}

private struct PeerRow: View {
    let peer: PeerSnapshot
    @Binding var gain: Double
    @State private var previousGain: Double = 1

    var body: some View {
        HStack(spacing: 13) {
            Image(systemName: peer.architecture.contains("aarch") || peer.architecture.contains("arm") ? "cpu" : "desktopcomputer")
                .font(.system(size: 19, weight: .regular))
                .foregroundStyle(Palette.secondary)
                .frame(width: 40, height: 40)
                .background(Palette.canvas, in: RoundedRectangle(cornerRadius: 10))
            VStack(alignment: .leading, spacing: 5) {
                Text(peer.name).font(.system(size: 13, weight: .medium)).lineLimit(1)
                Text("Linux · \(peer.architecture) · \(peer.host)")
                    .font(.system(size: 10)).foregroundStyle(Palette.secondary)
            }
            Spacer()
            HStack(spacing: 5) {
                Circle().fill(peer.online ? Palette.online : Palette.secondary.opacity(0.4)).frame(width: 5, height: 5)
                Text(peer.online ? (peer.playing ? "正在传送" : "已连接") : "已离线")
                    .font(.system(size: 10)).foregroundStyle(Palette.secondary)
            }
            .help("当前音频缓冲：\(peer.bufferedMilliseconds) ms")
            Button {
                if gain > 0 { previousGain = gain; gain = 0 } else { gain = previousGain }
            } label: {
                Image(systemName: gain > 0 ? "speaker.wave.2" : "speaker.slash").frame(width: 20)
            }
            .buttonStyle(.borderless).foregroundStyle(Palette.secondary).help("静音 / 恢复")
            .accessibilityLabel("\(peer.name) 静音 / 恢复")
            Slider(value: $gain, in: 0...1).controlSize(.small).frame(width: 90)
                .accessibilityLabel("\(peer.name) 音量")
            Text("\(Int(gain * 100))%")
                .font(.system(size: 10, design: .monospaced)).foregroundStyle(Palette.secondary).frame(width: 32)
        }
        .padding(.horizontal, 20).padding(.vertical, 14)
    }
}

private struct WhitePanel: ViewModifier {
    func body(content: Content) -> some View {
        content
            .background(.white, in: RoundedRectangle(cornerRadius: 14))
            .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Palette.line, lineWidth: 1).allowsHitTesting(false))
            .shadow(color: .black.opacity(0.015), radius: 4, x: 0, y: 2)
    }
}

private struct QuietButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 11, weight: .medium))
            .foregroundStyle(Palette.secondary)
            .padding(.horizontal, 11).padding(.vertical, 7)
            .background(configuration.isPressed ? Color.black.opacity(0.065) : Palette.canvas, in: RoundedRectangle(cornerRadius: 7))
    }
}

private struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .foregroundStyle(.white)
            .padding(.horizontal, 14).padding(.vertical, 10)
            .background(Palette.accent.opacity(configuration.isPressed ? 0.8 : 1), in: RoundedRectangle(cornerRadius: 8))
    }
}


private struct MicrophonePanel: View {
    @ObservedObject var model: AppModel
    var body: some View {
        VStack(alignment: .leading, spacing: 15) {
            HStack {
                Text("同步设备").font(.system(size: 12, weight: .medium))
                Picker("同步设备", selection: Binding(get: { model.microphonePeerID }, set: model.selectMicrophonePeer)) {
                    Text("选择 Linux 设备").tag(nil as UUID?)
                    ForEach(model.peers.filter(\.supportsMicrophone)) { peer in
                        Text(peer.name + (peer.online ? "" : " · 离线")).tag(peer.id as UUID?)
                    }
                }.labelsHidden().frame(width: 220)
                Spacer()
                if !model.receiving { Text("请先开启声音接收").font(.system(size: 11)).foregroundStyle(.orange) }
            }
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Label("Mac → Linux", systemImage: "mic").font(.system(size: 14, weight: .semibold))
                    Spacer()
                    Toggle("发送麦克风", isOn: Binding(get: { model.macMicrophoneEnabled }, set: model.enableMacMicrophone))
                        .toggleStyle(.switch).controlSize(.small).disabled(model.microphonePermissionPending)
                }
                HStack {
                    Text("Mac 输入").font(.system(size: 11)).foregroundStyle(Palette.secondary).frame(width: 70, alignment: .leading)
                    Picker("Mac 麦克风", selection: Binding(get: { model.microphoneDeviceUID }, set: model.selectMicrophoneDevice)) {
                        if model.microphoneDevices.isEmpty { Text("没有可用的物理麦克风").tag("") }
                        ForEach(model.microphoneDevices) { Text($0.name).tag($0.id) }
                    }.labelsHidden().frame(maxWidth: 270)
                    GeometryReader { proxy in
                        Capsule().fill(Palette.line).overlay(alignment: .leading) {
                            Capsule().fill(Palette.accent).frame(width: proxy.size.width * min(1, CGFloat(model.microphoneLevel) * 6))
                        }
                    }.frame(width: 75, height: 5)
                }
                Text("开启后，Linux 默认输入切换为 SoundFlow-Mac-Microphone；关闭后恢复。")
                    .font(.system(size: 11)).foregroundStyle(Palette.secondary)
            }.padding(18).modifier(WhitePanel())
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Label("Linux → Mac", systemImage: "mic.badge.plus").font(.system(size: 14, weight: .semibold))
                    Spacer()
                    Toggle("接收麦克风", isOn: Binding(get: { model.remoteMicrophoneEnabled }, set: model.enableRemoteMicrophone))
                        .toggleStyle(.switch).controlSize(.small)
                }
                HStack {
                    Text("Linux 输入").font(.system(size: 11)).foregroundStyle(Palette.secondary).frame(width: 70, alignment: .leading)
                    Picker("Linux 麦克风", selection: Binding(get: { model.remoteMicrophoneSource }, set: model.selectRemoteMicrophoneSource)) {
                        Text("默认物理麦克风").tag("")
                        ForEach(model.microphonePeer?.microphoneSources ?? []) { Text($0.description).tag($0.name) }
                    }.labelsHidden().frame(maxWidth: 270)
                }
                if let peer = model.microphonePeer, peer.online && peer.microphoneSources.isEmpty {
                    Text("此设备尚未检测到已接入的物理麦克风；连接后会自动识别。")
                        .font(.system(size: 11)).foregroundStyle(Palette.secondary)
                }
                if model.virtualMicrophoneDevices.isEmpty {
                    HStack(spacing: 10) {
                        Text("Mac 应用接收远端麦克风需要虚拟音频驱动。")
                            .font(.system(size: 11)).foregroundStyle(Palette.secondary)
                        Button("安装 BlackHole 2ch", action: model.installVirtualMicrophoneDriver)
                            .buttonStyle(QuietButtonStyle())
                    }
                } else {
                    HStack {
                        Text("Mac 虚拟输入").font(.system(size: 11)).foregroundStyle(Palette.secondary).frame(width: 70, alignment: .leading)
                        Picker("Mac 虚拟麦克风", selection: Binding(get: { model.virtualMicrophoneUID }, set: model.selectVirtualMicrophone)) {
                            ForEach(model.virtualMicrophoneDevices) { Text($0.name).tag($0.id) }
                        }.labelsHidden().frame(maxWidth: 270)
                    }
                    Text("在 Mac 的会议或录音应用中选择这个虚拟输入；声音不会回放到耳机。")
                        .font(.system(size: 11)).foregroundStyle(Palette.secondary)
                }
                if let status = model.microphonePeer?.microphoneStatus, !status.isEmpty {
                    Text(status).font(.system(size: 11)).foregroundStyle(.orange)
                }
            }.padding(18).modifier(WhitePanel())
            Text("两路可独立开启。使用蓝牙麦克风时，播放音质可能发生变化。")
                .font(.system(size: 10)).foregroundStyle(Palette.secondary)
        }.frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

