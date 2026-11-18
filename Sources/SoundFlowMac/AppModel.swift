import AppKit
import Combine
import ServiceManagement
import SoundFlowCore
import AVFoundation

@MainActor
final class AppModel: ObservableObject {
    @Published var peers: [PeerSnapshot] = []
    @Published var receiving = false
    @Published var error: String?
    @Published var outputName = "系统默认输出"
    @Published var copied = false
    @Published var master: Double = 1 { didSet { receiver?.setMaster(Float(master)); UserDefaults.standard.set(master, forKey: "master") } }
    @Published var buffer: Int = 60 { didSet { receiver?.setBuffer(milliseconds: buffer); UserDefaults.standard.set(buffer, forKey: "buffer") } }
    @Published var launchAtLogin = false
    @Published var microphonePeerID: UUID?
    @Published var microphoneDevices: [AudioDevice] = []
    @Published var virtualMicrophoneDevices: [AudioDevice] = []
    @Published var microphoneDeviceUID = ""
    @Published var virtualMicrophoneUID = ""
    @Published var remoteMicrophoneSource = ""
    @Published var macMicrophoneEnabled = false
    @Published var remoteMicrophoneEnabled = false
    @Published var microphoneLevel: Float = 0
    @Published var microphonePermissionPending = false
    private var receiver: Receiver?
    private var output: AudioOutput?
    private var microphoneInput: AudioInput?
    private var virtualMicrophoneOutput: AudioOutput?
    private var timer: Timer?
    private var secret: Data?
    private var remembered: [String: Float] = [:]
    private var applied: Set<UUID> = []
    private let verifyMicrophones = CommandLine.arguments.contains("--verify-microphones")
    private var verificationStarted = false

    var onlineCount: Int { peers.filter(\.online).count }
    var totalName: String { Host.current().localizedName ?? "我的 Mac" }

    init() {
        let defaults = UserDefaults.standard
        if defaults.object(forKey: "master") != nil { master = defaults.double(forKey: "master") }
        if defaults.object(forKey: "buffer") != nil { buffer = defaults.integer(forKey: "buffer") }
        remembered = (defaults.dictionary(forKey: "gains") ?? [:]).compactMapValues { ($0 as? NSNumber)?.floatValue }
        launchAtLogin = SMAppService.mainApp.status == .enabled
        start()
        timer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
    }
    func start() {
        guard !receiving else { return }
        do {
            let key = try KeyStore.loadOrCreate(); secret = key
            let defaults = UserDefaults.standard
            let identity = defaults.string(forKey: "receiverID").flatMap(UUID.init(uuidString:)) ?? UUID()
            defaults.set(identity.uuidString, forKey: "receiverID")
            let backend = Receiver(secret: key, id: identity, name: totalName, targetMilliseconds: buffer)
            backend.setMaster(Float(master))
            let player = AudioOutput(mixer: backend.mixer)
            player.onError = { [weak self] message in self?.error = message; self?.stop() }
            try player.start()
            do { try backend.start() } catch { player.stop(); throw error }
            receiver = backend; output = player; receiving = true; error = nil; applied = []
            refresh()
        } catch {
            self.error = "启动失败：\(error.localizedDescription)"
            receiving = false
        }
    }
    func stop() {
        enableMacMicrophone(false); enableRemoteMicrophone(false)
        // Stop the render callback before releasing the mixer's storage.
        output?.stop(); output = nil; receiver?.stop(); receiver = nil
        receiving = false; peers = []
    }
    func toggle() { receiving ? stop() : start() }
    func refresh() {
        peers = receiver?.snapshots() ?? []
        outputName = AudioOutput.defaultDeviceName()
        microphoneDevices = AudioDevices.microphones
        virtualMicrophoneDevices = AudioDevices.virtualOutputs
        if microphoneDeviceUID.isEmpty { microphoneDeviceUID = microphoneDevices.first?.id ?? "" }
        if virtualMicrophoneUID.isEmpty { virtualMicrophoneUID = virtualMicrophoneDevices.first?.id ?? "" }
        if microphonePeerID == nil { microphonePeerID = peers.first(where: { $0.online && $0.supportsMicrophone })?.id }
        if macMicrophoneEnabled && !microphoneDevices.contains(where: { $0.id == microphoneDeviceUID }) {
            enableMacMicrophone(false); error = "所选 Mac 麦克风已断开，请重新选择输入设备"
        }
        if remoteMicrophoneEnabled && !virtualMicrophoneDevices.contains(where: { $0.id == virtualMicrophoneUID }) {
            enableRemoteMicrophone(false); error = "虚拟麦克风设备已断开，请检查音频驱动"
        }
        for peer in peers where !applied.contains(peer.id) {
            if let gain = remembered[peer.id.uuidString] { receiver?.setGain(gain, for: peer.id) }
            applied.insert(peer.id)
        }
        if verifyMicrophones {
            if !verificationStarted, let peer = peers.first(where: { $0.online && $0.supportsMicrophone }),
                !microphoneDevices.isEmpty, !virtualMicrophoneDevices.isEmpty {
                verificationStarted = true; microphonePeerID = peer.id
                microphoneDeviceUID = microphoneDevices.first?.id ?? microphoneDeviceUID
                enableMacMicrophone(true); enableRemoteMicrophone(true)
            }
            // Explicit developer verification mode: metadata only, never record speech or keys.
            let status: [String: Any] = ["receiving": receiving, "mac_microphone": macMicrophoneEnabled,
                "linux_microphone": remoteMicrophoneEnabled, "input_level": microphoneLevel,
                "input": microphoneDevices.first(where: { $0.id == microphoneDeviceUID })?.name ?? "",
                "virtual_input": virtualMicrophoneDevices.first(where: { $0.id == virtualMicrophoneUID })?.name ?? "",
                "microphone_packets": microphonePeer?.microphonePackets ?? 0,
                "peer": microphonePeer?.host ?? "", "error": error ?? "",
                "linux_status": microphonePeer?.microphoneStatus ?? ""]
            let path = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
                .appendingPathComponent("app.soundflow.receiver", isDirectory: true)
            try? FileManager.default.createDirectory(at: path, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            if let data = try? JSONSerialization.data(withJSONObject: status) {
                let url = path.appendingPathComponent("microphone-verification.json")
                try? data.write(to: url, options: .atomic)
                try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
            }
        }
    }
    func gain(for id: UUID) -> Double {
        Double(remembered[id.uuidString] ?? peers.first(where: { $0.id == id })?.gain ?? 1)
    }
    func setGain(_ value: Double, id: UUID) {
        remembered[id.uuidString] = Float(value)
        receiver?.setGain(Float(value), for: id)
        UserDefaults.standard.set(remembered, forKey: "gains")
        objectWillChange.send()
    }
    func copyCode() {
        guard let secret else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(Wire.connectionCode(secret), forType: .string)
        copied = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) { [weak self] in self?.copied = false }
    }
    func setLogin(_ value: Bool) {
        do {
            if value { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            launchAtLogin = SMAppService.mainApp.status == .enabled
            if SMAppService.mainApp.status == .requiresApproval { SMAppService.openSystemSettingsLoginItems() }
        } catch { self.error = "登录启动设置失败：\(error.localizedDescription)" }
    }
    func openSoundSettings() {
        NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.Sound-Settings.extension")!)
    }

    var microphonePeer: PeerSnapshot? { peers.first { $0.id == microphonePeerID } }
    func selectMicrophonePeer(_ id: UUID?) {
        let send = macMicrophoneEnabled, receive = remoteMicrophoneEnabled
        enableMacMicrophone(false); enableRemoteMicrophone(false)
        microphonePeerID = id; remoteMicrophoneSource = ""
        if send { enableMacMicrophone(true) }
        if receive { enableRemoteMicrophone(true) }
    }
    func selectMicrophoneDevice(_ uid: String) {
        let active = macMicrophoneEnabled
        enableMacMicrophone(false); microphoneDeviceUID = uid
        if active { enableMacMicrophone(true) }
    }
    func selectRemoteMicrophoneSource(_ source: String) {
        remoteMicrophoneSource = source
        if remoteMicrophoneEnabled { receiver?.setRemoteMicrophone(peer: microphonePeerID, source: source) }
    }
    func selectVirtualMicrophone(_ uid: String) {
        let active = remoteMicrophoneEnabled
        enableRemoteMicrophone(false); virtualMicrophoneUID = uid
        if active { enableRemoteMicrophone(true) }
    }
    func enableMacMicrophone(_ enabled: Bool) {
        if !enabled {
            microphonePermissionPending = false; macMicrophoneEnabled = false
            receiver?.setMacMicrophoneTarget(nil)
            microphoneInput?.stop(); microphoneInput = nil; microphoneLevel = 0
            return
        }
        guard receiving, let peer = microphonePeer, peer.online, peer.supportsMicrophone else {
            error = "请先选择一台在线且支持麦克风同步的 Linux 设备"; return
        }
        microphonePermissionPending = true
        AVCaptureDevice.requestAccess(for: .audio) { [weak self] granted in
            Task { @MainActor in
                guard let self, self.microphonePermissionPending else { return }
                self.microphonePermissionPending = false
                guard granted else { self.error = "请在系统设置的「麦克风」中允许 SoundFlow"; return }
                guard self.receiving, let backend = self.receiver,
                    let device = self.microphoneDevices.first(where: { $0.id == self.microphoneDeviceUID }),
                    let target = self.microphonePeerID else { self.error = "所选麦克风或接收设备不可用"; return }
                do {
                    let input = AudioInput()
                    input.onPacket = { [weak backend] in backend?.sendMacMicrophone($0) }
                    input.onLevel = { [weak self] level in DispatchQueue.main.async { self?.microphoneLevel = level } }
                    input.onError = { [weak self] message in DispatchQueue.main.async {
                        self?.enableMacMicrophone(false); self?.error = "麦克风采集失败：\(message)"
                    } }
                    try input.start(deviceUID: device.id)
                    self.microphoneInput = input; backend.setMacMicrophoneTarget(target)
                    self.macMicrophoneEnabled = true; self.error = nil
                } catch { self.error = "无法打开 Mac 麦克风：\(error.localizedDescription)" }
            }
        }
    }
    func enableRemoteMicrophone(_ enabled: Bool) {
        if !enabled {
            remoteMicrophoneEnabled = false; receiver?.setRemoteMicrophone(peer: nil)
            virtualMicrophoneOutput?.stop(); virtualMicrophoneOutput = nil; return
        }
        guard receiving, let peer = microphonePeer, peer.online, peer.supportsMicrophone, let backend = receiver else {
            error = "请先选择在线的 Linux 设备"; return
        }
        guard let device = virtualMicrophoneDevices.first(where: { $0.id == virtualMicrophoneUID }) else {
            error = "Mac 尚未安装虚拟音频驱动。请安装 BlackHole 2ch 后重新选择虚拟麦克风。"; return
        }
        do {
            let player = AudioOutput(mixer: backend.microphoneMixer, deviceID: device.deviceID)
            player.onError = { [weak self] message in self?.enableRemoteMicrophone(false); self?.error = message }
            try player.start(); virtualMicrophoneOutput = player
            backend.setRemoteMicrophone(peer: peer.id, source: remoteMicrophoneSource)
            remoteMicrophoneEnabled = true; error = nil
        } catch { self.error = "无法启动虚拟麦克风：\(error.localizedDescription)" }
    }
    func installVirtualMicrophoneDriver() {
        let bundled = Bundle.main.resourceURL?.appendingPathComponent("BlackHole2ch-0.7.1.pkg")
        let local = Bundle.main.bundleURL.deletingLastPathComponent().appendingPathComponent("Extras/BlackHole2ch-0.7.1.pkg")
        if let url = [bundled, local].compactMap({ $0 }).first(where: { FileManager.default.fileExists(atPath: $0.path) }) {
            NSWorkspace.shared.open(url)
        } else { NSWorkspace.shared.open(URL(string: "https://existential.audio/blackhole/")!) }
    }
}
