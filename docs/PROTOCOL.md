# SoundFlow v1 protocol

## Discovery

IPv4 UDP 42790, multicast 239.255.42.99 (TTL 1) plus broadcast 255.255.255.255. Client binds an ephemeral UDP port and sends JSON:

```json
{"type":"soundflow-discover-v1","request":"random-request-id"}
```

Receiver responds to the source endpoint with `soundflow-offer-v1`, matching `request`, receiver UUID, name, audio `port`, protocol `version: 1`, and a random `challenge`. Challenge validity is 30 seconds, bound to the requesting IPv4 address, and required when establishing a new sender session. Discovery exposes the Mac name and UUID, never the key, Linux device name or audio. The offer is untrusted until an encrypted ACK proves possession of the key and matches the saved receiver UUID.

## Encrypted datagrams

Every message is at most 1400 bytes. Header (37 bytes) is authenticated associated data:

| Offset | Bytes | Meaning |
|---|---:|---|
| 0 | 4 | ASCII `SF01` |
| 4 | 1 | kind: 1 hello, 2 system audio, 3 ACK, 4 Mac microphone, 5 Linux microphone |
| 5 | 16 | sender UUID bytes, canonical UUID byte order |
| 21 | 8 | random session salt, renewed for each stream connection |
| 29 | 8 | unsigned big-endian sequence, strictly increasing per session |
| 37 | variable | ChaCha20-Poly1305 ciphertext |
| end − 16 | 16 | authentication tag |

The connection key is 32 cryptographically random bytes. UI encoding is `SF1-` + unpadded URL-safe Base64. Session key: HKDF-SHA256(input=connection key, salt=sender UUID || session salt, info=`SoundFlow-v1-client` or `SoundFlow-v1-server`, length=32). The directions have different keys. Nonce: four zero bytes followed by big-endian sequence. The server encrypts one ACK for each accepted, non-replayed hello and echoes its sequence. Audio and device names are never plaintext.

Hello JSON contains `name`, `arch`, `platform`, `rate:48000`, `channels:2`, `format:s16le`, `receiver` UUID and discovery `challenge`. ACK contains receiver `id`, name and audio format. Only authenticated hello creates a peer. Only an authenticated ACK for an outstanding hello starts Linux audio routing. ACKs are checked against the saved receiver UUID and actual endpoint.

Audio payload: 8-byte big-endian frame index, then exactly 240 frames of interleaved stereo s16le (960 PCM bytes). Frame index advances by 240 for each emitted packet. Datagrams are 1021 bytes, below the standard Ethernet MTU. Audio bandwidth is 200 datagrams/s, approximately 1.68 Mbit/s per sender including IPv4/UDP headers.

The receiver keeps a 256-sequence replay window and permits reordering. Session switches retire the previous salt and require a fresh valid discovery challenge. Slots are bounded to 16 devices, UDP dispatch work is bounded, and initial untrusted message sizes are bounded. This is a LAN-oriented initial implementation, not an independently audited transport.

The group key is stored in macOS Keychain and a 0600 user configuration on Linux. A device possessing the group key can transmit as a member of the group. Per-device key revocation, receiver-key rotation UI and a PAKE short-code pairing flow are not included in v0.1.

## Microphones (v0.2)

Hello advertises `capabilities:["microphone-v1"]`, available physical `microphones` and `microphone_status`. ACK carries `request_microphone`, `microphone_source`, `mac_microphone`, and an authenticated `server_session` (8 random bytes, hex encoded). Microphone capture is off until the corresponding control is enabled.

Kind 5 (Linux → Mac) uses the client's session and monotonically increasing shared client sequence, with HKDF info `SoundFlow-v1-linux-microphone`. It is independently reordered and mixed into the virtual-input output graph, never the headphone playback graph.

Kind 4 (Mac → Linux) uses the random server session in its header and HKDF info `SoundFlow-v1-mac-microphone`. A global server send counter increases across peers. The random session changes with each Receiver instance, preventing nonce reuse after a Mac restart even if a Linux client session survives briefly. Linux only accepts the server session announced in a valid, outstanding hello's ACK; a separate 256-sequence replay window protects microphone packets.

Microphone PCM uses the same 8-byte frame index and 240-frame stereo s16le layout. Linux plays kind 4 into a private sink remapped to a virtual source, restoring the prior default source when disabled/disconnected. Local physical capture excludes output monitors, SoundFlow sources and inputs whose jack ports all explicitly report unavailable. Mac physical capture likewise excludes the virtual loopback device. No speech is recorded by the application.

## Playback

15 ms packet reordering wait; missing 5 ms packets are replaced with silence, and long gaps skip directly to newer data. A C SPSC ring per sender prebuffers 30/60/120 ms. Rendering makes no allocations or mutex calls. Consumer-side linear interpolation applies bounded ±0.5% rate correction to compensate clock drift; sustained backlog is trimmed. Per-device gain, master gain, fade-to-silence on underrun and stereo output clipping are applied in the mixer.

The Mac source node renders 48 kHz stereo float audio; AVAudioEngine adapts it to the system output device. Mac local audio stays in the normal system mixer; it is not recaptured or sent back to Linux.
