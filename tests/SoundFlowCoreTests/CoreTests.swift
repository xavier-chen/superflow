import XCTest
import CryptoKit
@testable import SoundFlowCore
import SoundFlowDSP

final class CoreTests: XCTestCase {
    let secret = Data(0..<32)
    let sender = UUID(uuidString: "00112233-4455-6677-8899-aabbccddeeff")!
    let session = Data(0..<8)

    func testMicrophoneKeysAreSeparatedFromSystemAndAcknowledgements() throws {
        let payload = Data(repeating: 42, count: 968)
        var bodies: Set<Data> = []
        for kind: Wire.Kind in [.audio, .ack, .macMicrophone, .linuxMicrophone] {
            let bytes = try Wire.seal(kind: kind, sender: sender, session: session, sequence: 10, payload: payload, secret: secret)
            XCTAssertEqual(try Wire.open(bytes, secret: secret).payload, payload)
            bodies.insert(Data(bytes.dropFirst(Wire.headerSize)))
        }
        XCTAssertEqual(bodies.count, 4)
    }

    func testEncryptedPacketsAuthenticateHeaderAndPayload() throws {
        let payload = Data("hello stereo".utf8)
        let packet = try Wire.seal(kind: .audio, sender: sender, session: session, sequence: 42, payload: payload, secret: secret)
        let decoded = try Wire.open(packet, secret: secret)
        XCTAssertEqual(decoded.sender, sender); XCTAssertEqual(decoded.sequence, 42)
        XCTAssertEqual(decoded.payload, payload)
        for index in [4, 10, 25, 36, 40, packet.count - 1] {
            var changed = packet; changed[index] ^= 1
            XCTAssertThrowsError(try Wire.open(changed, secret: secret))
        }
        XCTAssertThrowsError(try Wire.open(packet, secret: Data(repeating: 4, count: 32)))
        let ack = try Wire.seal(kind: .ack, sender: sender, session: session, sequence: 42, payload: payload, secret: secret)
        XCTAssertNotEqual(packet.suffix(payload.count + 16), ack.suffix(payload.count + 16))
    }

    func testCodeRoundTripAndMalformedPackets() throws {
        XCTAssertEqual(Wire.parseCode(Wire.connectionCode(secret)), secret)
        XCTAssertNil(Wire.parseCode("SF1-invalid"))
        XCTAssertNil(Wire.parseCode("hello"))
        for count in 0..<53 { XCTAssertThrowsError(try Wire.open(Data(repeating: 0, count: count), secret: secret)) }
    }

    func testReplayWindowAcceptsReorderingAndRejectsDuplicates() {
        var window = ReplayWindow()
        XCTAssertTrue(window.accept(100)); XCTAssertTrue(window.accept(99)); XCTAssertFalse(window.accept(99))
        XCTAssertTrue(window.accept(500)); XCTAssertFalse(window.accept(100))
        XCTAssertTrue(window.accept(499)); XCTAssertFalse(window.accept(500))
    }

    func testJitterReordersAndConcealsMissingPacket() {
        let jitter = JitterBuffer()
        let zero = Data(repeating: 0, count: 960), one = Data(repeating: 1, count: 960), two = Data(repeating: 2, count: 960)
        jitter.insert(frame: 240, pcm: one, now: 0)
        jitter.insert(frame: 0, pcm: zero, now: 0.005)
        jitter.insert(frame: 720, pcm: two, now: 0.010)
        var result: [Data] = []
        jitter.drain(now: 0.02) { result.append($0) }
        XCTAssertEqual(result, [zero, one])
        jitter.drain(now: 0.04) { result.append($0) }
        XCTAssertEqual(result, [zero, one, zero, two]); XCTAssertEqual(jitter.concealed, 1)
        jitter.insert(frame: 0, pcm: zero, now: 0.05)
        jitter.drain(now: 1) { result.append($0) }
        XCTAssertEqual(result.count, 4)
    }

    func testJitterBoundsLongGaps() {
        let jitter = JitterBuffer(); var count = 0
        jitter.insert(frame: 0, pcm: Data(repeating: 0, count: 960), now: 0)
        jitter.insert(frame: 240 * 100000, pcm: Data(repeating: 0, count: 960), now: 0)
        jitter.drain(now: 0.02) { _ in count += 1 }
        jitter.drain(now: 0.04) { _ in count += 1 }
        XCTAssertEqual(count, 2); XCTAssertEqual(jitter.concealed, 99999)
    }

    private func pcm(_ value: Int16, frames: Int) -> Data {
        let low = UInt8(truncatingIfNeeded: value), high = UInt8(truncatingIfNeeded: value >> 8)
        return Data((0..<(frames * 2)).flatMap { _ in [low, high] })
    }

    func testMixerCombinesLimitsAndMutesStreams() throws {
        let mixer = try XCTUnwrap(sf_mixer_create(2880)); defer { sf_mixer_destroy(mixer) }
        let data = pcm(24576, frames: 8000)
        for slot: UInt32 in [0, 1] {
            data.withUnsafeBytes { XCTAssertEqual(sf_mixer_write_s16le(mixer, slot, $0.bindMemory(to: UInt8.self).baseAddress!, 8000), 8000) }
        }
        var left = [Float](repeating: 0, count: 1024), right = left
        sf_mixer_render(mixer, &left, &right, 1024)
        XCTAssertEqual(left[1000], 1, accuracy: 0.001); XCTAssertEqual(left, right)
        sf_mixer_set_gain(mixer, 1, 0)
        sf_mixer_render(mixer, &left, &right, 1024)
        XCTAssertEqual(left[1000], 0.75, accuracy: 0.001)
        sf_mixer_set_master(mixer, 0)
        sf_mixer_render(mixer, &left, &right, 1024)
        XCTAssertTrue(left.allSatisfy { $0 == 0 })
    }

    func testMixerPrebuffersAndRecoversFromUnderrun() throws {
        let mixer = try XCTUnwrap(sf_mixer_create(960)); defer { sf_mixer_destroy(mixer) }
        var left = [Float](repeating: 0, count: 240), right = left
        let small = pcm(16000, frames: 240)
        small.withUnsafeBytes { _ = sf_mixer_write_s16le(mixer, 0, $0.bindMemory(to: UInt8.self).baseAddress!, 240) }
        sf_mixer_render(mixer, &left, &right, 240)
        XCTAssertTrue(left.allSatisfy { $0 == 0 })
        for _ in 0..<4 { small.withUnsafeBytes { _ = sf_mixer_write_s16le(mixer, 0, $0.bindMemory(to: UInt8.self).baseAddress!, 240) } }
        sf_mixer_render(mixer, &left, &right, 240)
        XCTAssertGreaterThan(left.last!, 0.3)
        for _ in 0..<20 { sf_mixer_render(mixer, &left, &right, 240) }
        XCTAssertEqual(sf_mixer_underruns(mixer, 0), 1)
        XCTAssertLessThan(abs(left.last!), 0.0001)
    }

    func testClockDriftStaysBounded() throws {
        let mixer = try XCTUnwrap(sf_mixer_create(2880)); defer { sf_mixer_destroy(mixer) }
        let block = pcm(8000, frames: 241)
        var left = [Float](repeating: 0, count: 240), right = left
        // Simulate a fast sender for 150 seconds, without wall-clock waiting.
        for tick in 0..<30000 {
            let frames = tick % 10 == 0 ? 241 : 240
            block.withUnsafeBytes { _ = sf_mixer_write_s16le(mixer, 0, $0.bindMemory(to: UInt8.self).baseAddress!, frames) }
            sf_mixer_render(mixer, &left, &right, 240)
        }
        XCTAssertLessThan(sf_mixer_buffered(mixer, 0), 6000)
        XCTAssertEqual(sf_mixer_underruns(mixer, 0), 0)
    }
}
