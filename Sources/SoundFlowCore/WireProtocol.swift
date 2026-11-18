import Foundation
import CryptoKit

public enum Wire {
    public static let discoveryPort: UInt16 = 42790
    public static let audioPort: UInt16 = 42791
    public static let multicast = "239.255.42.99"
    public static let frames = 240
    public static let headerSize = 37
    public enum Kind: UInt8 { case hello = 1, audio = 2, ack = 3, macMicrophone = 4, linuxMicrophone = 5 }

    private static func key(secret: Data, sender: UUID, session: Data, kind: Kind) -> SymmetricKey {
        if kind == .macMicrophone || kind == .linuxMicrophone {
            return HKDF<SHA256>.deriveKey(inputKeyMaterial: SymmetricKey(data: secret),
                salt: uuidData(sender) + session,
                info: Data((kind == .macMicrophone ? "SoundFlow-v1-mac-microphone" : "SoundFlow-v1-linux-microphone").utf8), outputByteCount: 32)
        }
        return sessionKey(secret: secret, sender: sender, session: session, reply: kind == .ack)
    }

    public struct Packet {
        public let kind: Kind
        public let sender: UUID
        public let session: Data
        public let sequence: UInt64
        public let payload: Data
    }

    public static func sessionKey(secret: Data, sender: UUID, session: Data, reply: Bool) -> SymmetricKey {
        HKDF<SHA256>.deriveKey(inputKeyMaterial: SymmetricKey(data: secret),
            salt: uuidData(sender) + session,
            info: Data((reply ? "SoundFlow-v1-server" : "SoundFlow-v1-client").utf8), outputByteCount: 32)
    }

    public static func seal(kind: Kind, sender: UUID, session: Data, sequence: UInt64, payload: Data, secret: Data) throws -> Data {
        guard session.count == 8, secret.count == 32 else { throw WireError.malformed }
        let header = Data("SF01".utf8) + Data([kind.rawValue]) + uuidData(sender) + session + bigEndian(sequence)
        let nonce = try ChaChaPoly.Nonce(data: Data(repeating: 0, count: 4) + bigEndian(sequence))
        let box = try ChaChaPoly.seal(payload, using: key(secret: secret, sender: sender, session: session, kind: kind), nonce: nonce, authenticating: header)
        return header + box.ciphertext + box.tag
    }

    public static func open(_ data: Data, secret: Data) throws -> Packet {
        guard data.count >= headerSize + 16, data.count <= 1400, secret.count == 32,
            data.prefix(4) == Data("SF01".utf8), let kind = Kind(rawValue: data[4]) else { throw WireError.malformed }
        let bytes = Array(data[5..<21])
        let sender = UUID(uuid: (bytes[0],bytes[1],bytes[2],bytes[3],bytes[4],bytes[5],bytes[6],bytes[7],bytes[8],bytes[9],bytes[10],bytes[11],bytes[12],bytes[13],bytes[14],bytes[15]))
        let session = Data(data[21..<29])
        let sequence = uint64(data, at: 29)
        let nonce = try ChaChaPoly.Nonce(data: Data(repeating: 0, count: 4) + bigEndian(sequence))
        let box = try ChaChaPoly.SealedBox(nonce: nonce, ciphertext: data[headerSize..<(data.count - 16)], tag: data.suffix(16))
        let payload = try ChaChaPoly.open(box, using: key(secret: secret, sender: sender, session: session, kind: kind), authenticating: data.prefix(headerSize))
        return Packet(kind: kind, sender: sender, session: session, sequence: sequence, payload: payload)
    }

    public static func uuidData(_ value: UUID) -> Data { var raw = value.uuid; return withUnsafeBytes(of: &raw) { Data($0) } }
    public static func bigEndian(_ value: UInt64) -> Data { var raw = value.bigEndian; return withUnsafeBytes(of: &raw) { Data($0) } }
    public static func uint64(_ data: Data, at offset: Int) -> UInt64 { data[offset..<(offset + 8)].reduce(0) { ($0 << 8) | UInt64($1) } }
    public static func connectionCode(_ data: Data) -> String {
        "SF1-" + data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
    }
    public static func parseCode(_ code: String) -> Data? {
        let text = code.trimmingCharacters(in: .whitespacesAndNewlines)
        guard text.hasPrefix("SF1-") else { return nil }
        var base64 = String(text.dropFirst(4)).replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        base64 += String(repeating: "=", count: (4 - base64.count % 4) % 4)
        guard let data = Data(base64Encoded: base64), data.count == 32 else { return nil }; return data
    }
    public enum WireError: Error { case malformed }
}

// A bounded sliding replay window permits reordered UDP packets without accepting duplicates.
public struct ReplayWindow {
    private var highest: UInt64?
    private var seen: Set<UInt64> = []
    public init() {}
    public mutating func accept(_ sequence: UInt64) -> Bool {
        if let high = highest, high > sequence && high - sequence >= 256 { return false }
        guard seen.insert(sequence).inserted else { return false }
        highest = max(highest ?? sequence, sequence)
        if let high = highest, high >= 256 { seen = seen.filter { $0 > high - 256 } }
        return true
    }
}
