import Foundation
import SoundFlowDSP

public struct RemoteMicrophoneSource: Identifiable {
    public var id: String { name }
    public let name: String
    public let description: String
}

public struct PeerSnapshot: Identifiable {
    public let id: UUID
    public let name: String
    public let architecture: String
    public let host: String
    public let online: Bool
    public let playing: Bool
    public let bufferedMilliseconds: Int
    public let received: UInt64
    public let concealed: UInt64
    public let underruns: UInt64
    public let gain: Float
    public let microphoneSources: [RemoteMicrophoneSource]
    public let microphoneStatus: String
    public let microphonePackets: UInt64
    public let supportsMicrophone: Bool
}

private final class Peer {
    let id: UUID
    let slot: UInt32
    var session: Data
    var name: String
    var architecture: String
    var host: String
    var lastSeen: TimeInterval
    var lastAudio: TimeInterval = 0
    var gain: Float = 1
    var replay = ReplayWindow()
    var jitter = JitterBuffer()
    var microphoneJitter = JitterBuffer()
    var microphoneRequested = false
    var microphoneSource = ""
    var microphoneSources: [RemoteMicrophoneSource] = []
    var microphoneStatus = ""
    var supportsMicrophone = false
    var endpoint: sockaddr_in?
    init(packet: Wire.Packet, slot: UInt32, name: String, arch: String, host: String, now: TimeInterval) {
        id = packet.sender; session = packet.session; self.slot = slot
        self.name = name; architecture = arch; self.host = host; lastSeen = now
    }
}

public final class Receiver {
    public let mixer: OpaquePointer
    public let microphoneMixer: OpaquePointer
    public let id: UUID
    public let name: String
    private let secret: Data
    private let audioPort: UInt16
    private let discoveryPort: UInt16
    private let queue = DispatchQueue(label: "app.soundflow.network", qos: .userInteractive)
    private let queueKey = DispatchSpecificKey<Bool>()
    private var discovery: UDPSocket?
    private var audio: UDPSocket?
    private var timer: DispatchSourceTimer?
    private var peers: [UUID: Peer] = [:]
    // Prevent a delayed old hello from rolling a sender back to an earlier session.
    private var retiredSessions: [String: TimeInterval] = [:]
    private var offers: [UInt32: TimeInterval] = [:]
    private var challenges: [String: (host: UInt32, time: TimeInterval)] = [:]
    private var running = false
    private let serverSession = Data(Wire.uuidData(UUID()).prefix(8))
    private var macMicrophoneTarget: UUID?
    private var remoteMicrophoneTarget: UUID?
    private var remoteMicrophoneSource = ""
    private var macMicrophoneSequence: UInt64 = 0
    private var macMicrophoneFrame: UInt64 = 0

    public init(secret: Data, id: UUID, name: String, targetMilliseconds: Int = 60,
                audioPort: UInt16 = Wire.audioPort, discoveryPort: UInt16 = Wire.discoveryPort) {
        precondition(secret.count == 32)
        self.secret = secret; self.id = id; self.name = name
        self.audioPort = audioPort; self.discoveryPort = discoveryPort
        mixer = sf_mixer_create(UInt32(max(20, min(200, targetMilliseconds)) * 48))!
        microphoneMixer = sf_mixer_create(UInt32(max(20, min(200, targetMilliseconds)) * 48))!
        queue.setSpecific(key: queueKey, value: true)
    }

    public func start() throws {
        try queue.sync {
            guard !running else { return }
            do {
                discovery = try UDPSocket(port: discoveryPort, queue: queue, multicast: true) { [weak self] in self?.discover($0, from: $1) }
                audio = try UDPSocket(port: audioPort, queue: queue) { [weak self] in self?.receive($0, from: $1) }
                let clock = DispatchSource.makeTimerSource(queue: queue)
                clock.schedule(deadline: .now(), repeating: .milliseconds(5), leeway: .milliseconds(1))
                clock.setEventHandler { [weak self] in self?.tick() }
                timer = clock; running = true; clock.resume()
            } catch {
                discovery?.close(); discovery = nil; audio?.close(); audio = nil
                throw error
            }
        }
    }

    public func stop() {
        let cleanup = { [self] in
            running = false
            timer?.cancel(); timer = nil
            discovery?.close(); discovery = nil; audio?.close(); audio = nil
            for peer in peers.values { sf_mixer_flush(mixer, peer.slot); sf_mixer_flush(microphoneMixer, peer.slot) }
        }
        if DispatchQueue.getSpecific(key: queueKey) == true { cleanup() } else { queue.sync(execute: cleanup) }
    }
    deinit { stop(); sf_mixer_destroy(mixer); sf_mixer_destroy(microphoneMixer) }

    public func snapshots() -> [PeerSnapshot] {
        queue.sync {
            let now = ProcessInfo.processInfo.systemUptime
            return peers.values.map { peer in
                PeerSnapshot(id: peer.id, name: peer.name, architecture: peer.architecture, host: peer.host,
                    online: running && now - peer.lastSeen < 3,
                    playing: running && now - peer.lastAudio < 1,
                    bufferedMilliseconds: Int(sf_mixer_buffered(mixer, peer.slot)) / 48,
                    received: peer.jitter.received, concealed: peer.jitter.concealed,
                    underruns: sf_mixer_underruns(mixer, peer.slot), gain: peer.gain,
                    microphoneSources: peer.microphoneSources, microphoneStatus: peer.microphoneStatus,
                    microphonePackets: peer.microphoneJitter.received, supportsMicrophone: peer.supportsMicrophone)
            }.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        }
    }
    public func setGain(_ value: Float, for id: UUID) {
        queue.async { [self] in
            guard let peer = peers[id] else { return }
            peer.gain = value; sf_mixer_set_gain(mixer, peer.slot, value)
        }
    }
    public func setMaster(_ value: Float) { sf_mixer_set_master(mixer, value) }
    public func setBuffer(milliseconds: Int) { sf_mixer_set_target(mixer, UInt32(max(20, min(200, milliseconds)) * 48)) }

    public func setRemoteMicrophone(peer id: UUID?, source: String = "") {
        queue.async { [self] in
            remoteMicrophoneTarget = id; remoteMicrophoneSource = source
            for peer in peers.values {
                peer.microphoneRequested = peer.id == id
                peer.microphoneSource = peer.id == id ? source : ""
                peer.microphoneJitter = JitterBuffer()
                sf_mixer_flush(microphoneMixer, peer.slot)
            }
        }
    }
    public func setMacMicrophoneTarget(_ id: UUID?) { queue.async { [self] in macMicrophoneTarget = id } }
    public func sendMacMicrophone(_ pcm: Data) {
        guard pcm.count == Wire.frames * 4 else { return }
        queue.async { [self] in
            guard running, let id = macMicrophoneTarget, let peer = peers[id], peer.supportsMicrophone,
                ProcessInfo.processInfo.systemUptime - peer.lastSeen < 3, let endpoint = peer.endpoint else { return }
            let sequence = macMicrophoneSequence; macMicrophoneSequence += 1
            let payload = Wire.bigEndian(macMicrophoneFrame) + pcm
            macMicrophoneFrame += UInt64(Wire.frames)
            if let packet = try? Wire.seal(kind: .macMicrophone, sender: peer.id, session: serverSession,
                sequence: sequence, payload: payload, secret: secret) { audio?.send(packet, to: endpoint) }
        }
    }

    private func discover(_ data: Data, from address: sockaddr_in) {
        guard running, data.count <= 512,
            let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            json["type"] as? String == "soundflow-discover-v1",
            let request = json["request"] as? String, request.count <= 64 else { return }
        let now = ProcessInfo.processInfo.systemUptime
        let ip = address.sin_addr.s_addr
        if let last = offers[ip], now - last < 0.1 { return }
        if offers.count > 256 { offers = offers.filter { now - $0.value < 10 } }
        offers[ip] = now
        if challenges.count > 512 { challenges = challenges.filter { now - $0.value.time < 30 } }
        guard challenges.count <= 1024 else { return }
        let challenge = UUID().uuidString.lowercased()
        challenges[challenge] = (ip, now)
        let response: [String: Any] = ["type": "soundflow-offer-v1", "id": id.uuidString.lowercased(),
            "name": name, "port": Int(audioPort), "request": request, "version": 1, "challenge": challenge]
        if let bytes = try? JSONSerialization.data(withJSONObject: response) { discovery?.send(bytes, to: address) }
    }

    private func receive(_ data: Data, from address: sockaddr_in) {
        guard running, let packet = try? Wire.open(data, secret: secret),
            packet.kind != .ack, packet.kind != .macMicrophone else { return }
        let now = ProcessInfo.processInfo.systemUptime
        let sessionID = packet.sender.uuidString + packet.session.base64EncodedString()
        guard retiredSessions[sessionID] == nil else { return }
        var peer = peers[packet.sender]
        if packet.kind == .hello {
            guard let json = try? JSONSerialization.jsonObject(with: packet.payload) as? [String: Any],
                let peerName = json["name"] as? String, !peerName.isEmpty, peerName.count <= 80,
                json["rate"] as? Int == 48000, json["channels"] as? Int == 2,
                json["format"] as? String == "s16le",
                json["receiver"] as? String == id.uuidString.lowercased() else { return }
            if peer == nil || peer!.session != packet.session {
                guard let challenge = json["challenge"] as? String, let issued = challenges[challenge],
                    issued.host == address.sin_addr.s_addr, now - issued.time < 30 else { return }
            }
            let arch = String((json["arch"] as? String ?? "Linux").prefix(40))
            if peer == nil {
                let used = Set(peers.values.map(\.slot))
                guard let slot = (UInt32(0)..<UInt32(SF_MAX_STREAMS)).first(where: { !used.contains($0) }) else { return }
                let added = Peer(packet: packet, slot: slot, name: peerName, arch: arch, host: UDPSocket.host(address), now: now)
                peers[packet.sender] = added; peer = added
            } else if peer!.session != packet.session {
                // Keep a bounded tombstone list; unknown/replayed sessions cannot displace active peers.
                guard now - peer!.lastSeen > 2 else { return }
                retiredSessions[packet.sender.uuidString + peer!.session.base64EncodedString()] = now
                peer!.session = packet.session; peer!.replay = ReplayWindow(); peer!.jitter = JitterBuffer()
                peer!.microphoneJitter = JitterBuffer()
                sf_mixer_flush(mixer, peer!.slot)
                sf_mixer_flush(microphoneMixer, peer!.slot)
            }
            guard let peer, peer.replay.accept(packet.sequence) else { return }
            peer.name = peerName; peer.architecture = arch; peer.host = UDPSocket.host(address); peer.lastSeen = now
            peer.endpoint = address
            peer.microphoneRequested = remoteMicrophoneTarget == peer.id
            peer.microphoneSource = peer.microphoneRequested ? remoteMicrophoneSource : ""
            peer.supportsMicrophone = (json["capabilities"] as? [String] ?? []).contains("microphone-v1")
            peer.microphoneStatus = String((json["microphone_status"] as? String ?? "").prefix(160))
            peer.microphoneSources = ((json["microphones"] as? [[String: String]]) ?? []).prefix(8).compactMap {
                guard let name = $0["name"], name.count <= 160 else { return nil }
                return RemoteMicrophoneSource(name: name, description: String(($0["description"] ?? name).prefix(80)))
            }
            let ack: [String: Any] = ["id": id.uuidString.lowercased(), "name": name, "rate": 48000, "channels": 2,
                "request_microphone": peer.microphoneRequested, "microphone_source": peer.microphoneSource,
                "mac_microphone": macMicrophoneTarget == peer.id, "server_session": serverSession.map { String(format: "%02x", $0) }.joined()]
            if let payload = try? JSONSerialization.data(withJSONObject: ack),
                let bytes = try? Wire.seal(kind: .ack, sender: packet.sender, session: packet.session,
                    sequence: packet.sequence, payload: payload, secret: secret) { audio?.send(bytes, to: address) }
        } else if packet.kind == .audio || packet.kind == .linuxMicrophone {
            guard let peer, peer.session == packet.session, now - peer.lastSeen < 5,
                packet.payload.count == 8 + Wire.frames * 4,
                peer.replay.accept(packet.sequence) else { return }
            if packet.kind == .audio {
                peer.lastAudio = now
                peer.jitter.insert(frame: Wire.uint64(packet.payload, at: 0), pcm: Data(packet.payload.dropFirst(8)), now: now)
            } else if peer.microphoneRequested {
                peer.microphoneJitter.insert(frame: Wire.uint64(packet.payload, at: 0), pcm: Data(packet.payload.dropFirst(8)), now: now)
            }
        }
    }

    private func tick() {
        let now = ProcessInfo.processInfo.systemUptime
        for peer in peers.values {
            peer.jitter.drain(now: now) { pcm in
                pcm.withUnsafeBytes { bytes in
                    _ = sf_mixer_write_s16le(mixer, peer.slot, bytes.bindMemory(to: UInt8.self).baseAddress!, Wire.frames)
                }
            }
            peer.microphoneJitter.drain(now: now) { pcm in
                pcm.withUnsafeBytes { bytes in
                    _ = sf_mixer_write_s16le(microphoneMixer, peer.slot, bytes.bindMemory(to: UInt8.self).baseAddress!, Wire.frames)
                }
            }
            if now - peer.lastSeen > 120 {
                sf_mixer_flush(mixer, peer.slot)
                retiredSessions[peer.id.uuidString + peer.session.base64EncodedString()] = now
            }
        }
        peers = peers.filter { now - $0.value.lastSeen <= 120 }
        retiredSessions = retiredSessions.filter { now - $0.value < 600 }
        challenges = challenges.filter { now - $0.value.time < 30 }
    }
}
