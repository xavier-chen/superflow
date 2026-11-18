import AVFoundation
import AudioToolbox
import SoundFlowCore

/// AVCaptureSession follows Bluetooth input-format changes without forcing a hardware tap format.
final class AudioInput: NSObject, AVCaptureAudioDataOutputSampleBufferDelegate {
    private var session: AVCaptureSession?
    private let control = DispatchQueue(label: "app.soundflow.microphone-session", qos: .userInitiated)
    private let queue = DispatchQueue(label: "app.soundflow.microphone-conversion", qos: .userInteractive)
    private var converter: AVAudioConverter?
    private var inputFormat: AVAudioFormat?
    private let target = AVAudioFormat(standardFormatWithSampleRate: 48000, channels: 1)!
    private var packet = Data()
    private var stopped = true
    private var observer: NSObjectProtocol?
    var onPacket: ((Data) -> Void)?
    var onLevel: ((Float) -> Void)?
    var onError: ((String) -> Void)?

    func start(deviceUID: String) throws {
        stop()
        guard let device = AVCaptureDevice(uniqueID: deviceUID) else {
            throw NSError(domain: "SoundFlow", code: 1, userInfo: [NSLocalizedDescriptionKey: "所选麦克风不可用"])
        }
        let next = AVCaptureSession()
        let input = try AVCaptureDeviceInput(device: device)
        let output = AVCaptureAudioDataOutput()
        guard next.canAddInput(input), next.canAddOutput(output) else {
            throw NSError(domain: "SoundFlow", code: 2, userInfo: [NSLocalizedDescriptionKey: "无法配置麦克风采集会话"])
        }
        next.beginConfiguration()
        next.addInput(input); next.addOutput(output)
        output.setSampleBufferDelegate(self, queue: queue)
        next.commitConfiguration()
        queue.sync { stopped = false; converter = nil; inputFormat = nil; packet.removeAll(keepingCapacity: true) }
        session = next
        observer = NotificationCenter.default.addObserver(forName: .AVCaptureSessionRuntimeError, object: next, queue: nil) { [weak self] note in
            let error = note.userInfo?[AVCaptureSessionErrorKey] as? NSError
            self?.onError?(error?.localizedDescription ?? "麦克风采集会话已中断")
        }
        control.async { next.startRunning() }
    }

    func captureOutput(_ output: AVCaptureOutput, didOutput sampleBuffer: CMSampleBuffer, from connection: AVCaptureConnection) {
        guard !stopped, let description = CMSampleBufferGetFormatDescription(sampleBuffer),
              let raw = CMAudioFormatDescriptionGetStreamBasicDescription(description),
              let format = AVAudioFormat(streamDescription: raw) else { return }
        if inputFormat != format {
            converter = AVAudioConverter(from: format, to: target)
            inputFormat = format
            packet.removeAll(keepingCapacity: true)
        }
        let count = CMSampleBufferGetNumSamples(sampleBuffer)
        guard count > 0, let input = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(count)) else { return }
        input.frameLength = AVAudioFrameCount(count)
        let copied = CMSampleBufferCopyPCMDataIntoAudioBufferList(sampleBuffer, at: 0, frameCount: Int32(count), into: input.mutableAudioBufferList)
        guard copied == noErr, let converter else { return }
        let capacity = AVAudioFrameCount(Double(count) * 48000 / format.sampleRate) + 64
        guard let converted = AVAudioPCMBuffer(pcmFormat: target, frameCapacity: capacity) else { return }
        var supplied = false
        var error: NSError?
        let status = converter.convert(to: converted, error: &error) { _, state in
            if supplied { state.pointee = .noDataNow; return nil }
            supplied = true; state.pointee = .haveData; return input
        }
        if status == .error { onError?(error?.localizedDescription ?? "麦克风转换失败"); return }
        guard let samples = converted.floatChannelData?[0] else { return }
        var energy: Float = 0
        for index in 0..<Int(converted.frameLength) {
            let sample = max(-1, min(1, samples[index])); energy += sample * sample
            let value = Int16(max(-32768, min(32767, Int(sample * 32767))))
            let low = UInt8(truncatingIfNeeded: value), high = UInt8(truncatingIfNeeded: value >> 8)
            packet.append(contentsOf: [low, high, low, high])
            if packet.count == Wire.frames * 4 { onPacket?(packet); packet.removeAll(keepingCapacity: true) }
        }
        if converted.frameLength > 0 { onLevel?(sqrt(energy / Float(converted.frameLength))) }
    }

    func stop() {
        if let observer { NotificationCenter.default.removeObserver(observer) }
        observer = nil
        let previous = session
        session = nil
        control.sync { previous?.stopRunning() }
        queue.sync { stopped = true; converter = nil; inputFormat = nil; packet.removeAll(keepingCapacity: true) }
    }
    deinit { stop() }
}

