import Foundation

public final class JitterBuffer {
    private var pending: [UInt64: Data] = [:]
    private var expected: UInt64?
    private var firstArrival: TimeInterval?
    private var gapSince: TimeInterval?
    public private(set) var concealed: UInt64 = 0
    public private(set) var received: UInt64 = 0
    public init() {}

    public func insert(frame: UInt64, pcm: Data, now: TimeInterval) {
        guard pcm.count == Wire.frames * 4, frame % UInt64(Wire.frames) == 0,
            frame < UInt64.max - 48000,
            expected == nil || frame >= expected! else { return }
        if pending[frame] == nil { received += 1 }
        if pending.count >= 48 && pending[frame] == nil { return }
        pending[frame] = pcm
        if firstArrival == nil { firstArrival = now }
    }

    public func drain(now: TimeInterval, emit: (Data) -> Void) {
        guard let first = firstArrival, now - first >= 0.015 else { return }
        if expected == nil { expected = pending.keys.min() }
        var budget = 48
        while let frame = expected, budget > 0 {
            budget -= 1
            if let pcm = pending.removeValue(forKey: frame) {
                emit(pcm); expected = frame + UInt64(Wire.frames); gapSince = nil
            } else if let next = pending.keys.min(), next > frame {
                if gapSince == nil { gapSince = now }
                guard now - gapSince! >= 0.015 else { break }
                let distance = (next - frame) / UInt64(Wire.frames)
                // Long outages should never produce an unbounded silence backlog.
                if distance > 20 { concealed += distance; expected = next; continue }
                emit(Data(repeating: 0, count: Wire.frames * 4))
                concealed += 1; expected = frame + UInt64(Wire.frames)
            } else { break }
        }
    }
}
