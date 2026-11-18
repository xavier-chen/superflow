import Foundation
import SoundFlowCore
import SoundFlowDSP

// A diagnostic receiver uses the production decoder, jitter buffer and mixer,
// recording a WAV instead of opening hardware. It is also the cross-platform test endpoint.
func argument(_ name: String) -> String? {
    guard let index = CommandLine.arguments.firstIndex(of: name), index + 1 < CommandLine.arguments.count else { return nil }
    return CommandLine.arguments[index + 1]
}
guard let keyFile = argument("--key-file"), let recordPath = argument("--record"),
      let secret = Wire.parseCode((try? String(contentsOfFile: keyFile, encoding: .utf8)) ?? "") else {
    fputs("Usage: soundflow-probe --key-file PATH --record PATH [--duration 10] [--id UUID] [--discovery-port PORT] [--audio-port PORT]\n", stderr)
    exit(2)
}
let seconds = Double(argument("--duration") ?? "10") ?? 10
let id = UUID(uuidString: argument("--id") ?? "") ?? UUID()
let duplexTest = CommandLine.arguments.contains("--duplex-test")
var duplexPeer: UUID?
let receiver = Receiver(secret: secret, id: id, name: "SoundFlow Test Mac",
    audioPort: UInt16(argument("--audio-port") ?? "42791") ?? 42791,
    discoveryPort: UInt16(argument("--discovery-port") ?? "42790") ?? 42790)
do { try receiver.start() } catch { fputs("Receiver failed: \(error)\n", stderr); exit(1) }
print("READY \(id.uuidString.lowercased())")
fflush(stdout)
let rate: UInt32 = 48000
let totalFrames = Int(seconds * Double(rate))
var output = Data(capacity: totalFrames * 4 + 44)
func le<T: FixedWidthInteger>(_ value: T) -> Data { var v = value.littleEndian; return withUnsafeBytes(of: &v) { Data($0) } }
output.append(Data("RIFF".utf8)); output.append(le(UInt32(totalFrames * 4 + 36))); output.append(Data("WAVEfmt ".utf8))
output.append(le(UInt32(16))); output.append(le(UInt16(1))); output.append(le(UInt16(2)))
output.append(le(rate)); output.append(le(rate * 4)); output.append(le(UInt16(4))); output.append(le(UInt16(16)))
output.append(Data("data".utf8)); output.append(le(UInt32(totalFrames * 4)))
var left = [Float](repeating: 0, count: 240), right = left
let start = ProcessInfo.processInfo.systemUptime
for offset in stride(from: 0, to: totalFrames, by: 240) {
    let deadline = start + Double(offset) / Double(rate)
    let wait = deadline - ProcessInfo.processInfo.systemUptime
    if wait > 0 { Thread.sleep(forTimeInterval: wait) }
    let count = min(240, totalFrames - offset)
    if duplexTest {
        if duplexPeer == nil, let peer = receiver.snapshots().first(where: { $0.supportsMicrophone }) {
            duplexPeer = peer.id
            receiver.setMacMicrophoneTarget(peer.id)
            receiver.setRemoteMicrophone(peer: peer.id)
        }
        var pcm = Data(capacity: 960)
        for i in 0..<240 {
            let value = Int16(sin(2 * Double.pi * 660 * Double(offset + i) / 48000) * 7000)
            pcm.append(le(value)); pcm.append(le(value))
        }
        receiver.sendMacMicrophone(pcm)
    }
    sf_mixer_render(duplexTest ? receiver.microphoneMixer : receiver.mixer, &left, &right, UInt32(count))
    for i in 0..<count {
        output.append(le(Int16(max(-32768, min(32767, Int(left[i] * 32767))))))
        output.append(le(Int16(max(-32768, min(32767, Int(right[i] * 32767))))))
    }
}
let snapshots = receiver.snapshots()
receiver.stop()
try output.write(to: URL(fileURLWithPath: recordPath))
for peer in snapshots { print("PEER \(peer.name) arch=\(peer.architecture) packets=\(peer.received) concealed=\(peer.concealed) underruns=\(peer.underruns) microphone=\(peer.microphonePackets) status=\(peer.microphoneStatus)") }
print("RECORDED \(recordPath)")
