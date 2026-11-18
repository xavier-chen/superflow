import AVFoundation
import CoreAudio
import SoundFlowDSP

final class AudioOutput {
    private var engine: AVAudioEngine?
    private var source: AVAudioSourceNode?
    private var observer: NSObjectProtocol?
    private var restart: DispatchWorkItem?
    private let mixer: OpaquePointer
    private let deviceID: AudioDeviceID?
    var onError: ((String) -> Void)?
    private var active = false

    init(mixer: OpaquePointer, deviceID: AudioDeviceID? = nil) { self.mixer = mixer; self.deviceID = deviceID }
    func start() throws {
        active = true
        try rebuild()
    }
    private func rebuild() throws {
        if let observer { NotificationCenter.default.removeObserver(observer) }
        observer = nil
        engine?.stop()
        let next = AVAudioEngine()
        if let deviceID, let unit = next.outputNode.audioUnit {
            var id = deviceID
            let result = AudioUnitSetProperty(unit, kAudioOutputUnitProperty_CurrentDevice, kAudioUnitScope_Global, 0,
                &id, UInt32(MemoryLayout.size(ofValue: id)))
            guard result == noErr else { throw NSError(domain: NSOSStatusErrorDomain, code: Int(result)) }
        }
        let format = AVAudioFormat(standardFormatWithSampleRate: 48000, channels: 2)!
        let sharedMixer = mixer
        let node = AVAudioSourceNode(format: format) { _, _, count, buffers -> OSStatus in
            let list = UnsafeMutableAudioBufferListPointer(buffers)
            guard list.count >= 2, let left = list[0].mData, let right = list[1].mData else { return noErr }
            sf_mixer_render(sharedMixer, left.assumingMemoryBound(to: Float.self), right.assumingMemoryBound(to: Float.self), count)
            return noErr
        }
        next.attach(node)
        next.connect(node, to: next.mainMixerNode, format: format)
        // The main mixer converts our 48 kHz stream to the current output's format.
        next.prepare()
        try next.start()
        engine = next; source = node
        observer = NotificationCenter.default.addObserver(forName: .AVAudioEngineConfigurationChange, object: next, queue: .main) { [weak self] _ in
            guard let self, self.active else { return }
            self.restart?.cancel()
            let work = DispatchWorkItem { [weak self] in
                guard let self, self.active else { return }
                do { try self.rebuild() } catch { self.onError?("音频输出恢复失败：\(error.localizedDescription)") }
            }
            self.restart = work
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5, execute: work)
        }
    }
    func stop() {
        active = false; restart?.cancel(); restart = nil
        if let observer { NotificationCenter.default.removeObserver(observer) }
        observer = nil
        engine?.stop(); engine = nil; source = nil
    }
    deinit { stop() }

    static func defaultDeviceName() -> String {
        var device = AudioDeviceID(0)
        var size = UInt32(MemoryLayout<AudioDeviceID>.size)
        var property = AudioObjectPropertyAddress(mSelector: kAudioHardwarePropertyDefaultOutputDevice,
            mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
        guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &property, 0, nil, &size, &device) == noErr else { return "系统默认输出" }
        var name: Unmanaged<CFString>?
        size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
        property.mSelector = kAudioObjectPropertyName
        guard AudioObjectGetPropertyData(device, &property, 0, nil, &size, &name) == noErr else { return "系统默认输出" }
        return (name?.takeRetainedValue() as String?) ?? "系统默认输出"
    }
}
