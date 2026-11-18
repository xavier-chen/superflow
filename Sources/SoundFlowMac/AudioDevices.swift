import CoreAudio
import Foundation

struct AudioDevice: Identifiable, Equatable {
    let id: String
    let deviceID: AudioDeviceID
    let name: String
}

enum AudioDevices {
    static func all(input: Bool) -> [AudioDevice] {
        var property = AudioObjectPropertyAddress(mSelector: kAudioHardwarePropertyDevices,
            mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
        var size: UInt32 = 0
        guard AudioObjectGetPropertyDataSize(AudioObjectID(kAudioObjectSystemObject), &property, 0, nil, &size) == noErr else { return [] }
        var ids = [AudioDeviceID](repeating: 0, count: Int(size) / MemoryLayout<AudioDeviceID>.size)
        guard AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &property, 0, nil, &size, &ids) == noErr else { return [] }
        return ids.compactMap { id in
            var config = AudioObjectPropertyAddress(mSelector: kAudioDevicePropertyStreamConfiguration,
                mScope: input ? kAudioDevicePropertyScopeInput : kAudioDevicePropertyScopeOutput, mElement: kAudioObjectPropertyElementMain)
            var length: UInt32 = 0
            guard AudioObjectGetPropertyDataSize(id, &config, 0, nil, &length) == noErr, length > 0 else { return nil }
            let memory = UnsafeMutableRawPointer.allocate(byteCount: Int(length), alignment: 16)
            defer { memory.deallocate() }
            guard AudioObjectGetPropertyData(id, &config, 0, nil, &length, memory) == noErr else { return nil }
            let buffers = UnsafeMutableAudioBufferListPointer(memory.assumingMemoryBound(to: AudioBufferList.self))
            guard buffers.contains(where: { $0.mNumberChannels > 0 }),
                let name = string(id, selector: kAudioObjectPropertyName),
                let uid = string(id, selector: kAudioDevicePropertyDeviceUID) else { return nil }
            return AudioDevice(id: uid, deviceID: id, name: name)
        }.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }
    static func string(_ id: AudioObjectID, selector: AudioObjectPropertySelector) -> String? {
        var property = AudioObjectPropertyAddress(mSelector: selector, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
        var value: Unmanaged<CFString>?
        var size = UInt32(MemoryLayout.size(ofValue: value))
        guard AudioObjectGetPropertyData(id, &property, 0, nil, &size, &value) == noErr else { return nil }
        return value?.takeRetainedValue() as String?
    }
    static func isLoopback(_ device: AudioDevice) -> Bool {
        let value = (device.id + " " + device.name).lowercased()
        return value.contains("blackhole") || value.contains("soundflow")
    }
    static var microphones: [AudioDevice] { all(input: true).filter { !isLoopback($0) } }
    static var virtualOutputs: [AudioDevice] { all(input: false).filter(isLoopback) }
}
