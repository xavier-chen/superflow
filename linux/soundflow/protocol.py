"""Shared SF01 wire format. No audio or device names are sent unencrypted."""
import base64
import os
import struct
import uuid
from dataclasses import dataclass

from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.ciphers.aead import ChaCha20Poly1305
from cryptography.hazmat.primitives.kdf.hkdf import HKDF

DISCOVERY_PORT = 42790
AUDIO_PORT = 42791
MULTICAST = "239.255.42.99"
RATE, CHANNELS, FRAMES = 48000, 2, 240
PCM_BYTES = FRAMES * CHANNELS * 2
HELLO, AUDIO, ACK, MAC_MICROPHONE, LINUX_MICROPHONE = 1, 2, 3, 4, 5
HEADER = struct.Struct("!4sB16s8sQ")


def parse_code(code: str) -> bytes:
    text = code.strip()
    if not text.startswith("SF1-"):
        raise ValueError("连接密钥应以 SF1- 开头，请从 Mac 复制完整密钥。")
    try:
        raw = text[4:]
        value = base64.b64decode(raw + "=" * (-len(raw) % 4), altchars=b"-_", validate=True)
    except (ValueError, base64.binascii.Error) as exc:
        raise ValueError("连接密钥格式不正确。") from exc
    if len(value) != 32:
        raise ValueError("连接密钥长度不正确。")
    return value


def connection_code(secret: bytes) -> str:
    return "SF1-" + base64.urlsafe_b64encode(secret).decode().rstrip("=")


def session_key(secret: bytes, sender: uuid.UUID, session: bytes, reply=False, info=None) -> bytes:
    return HKDF(algorithm=hashes.SHA256(), length=32, salt=sender.bytes + session,
                info=info or (b"SoundFlow-v1-server" if reply else b"SoundFlow-v1-client")).derive(secret)


@dataclass
class Packet:
    kind: int
    sender: uuid.UUID
    session: bytes
    sequence: int
    payload: bytes


class Cipher:
    def __init__(self, secret: bytes, sender: uuid.UUID, session=None):
        self.sender = sender
        self.session = os.urandom(8) if session is None else session
        self.client = ChaCha20Poly1305(session_key(secret, sender, self.session))
        self.server = ChaCha20Poly1305(session_key(secret, sender, self.session, reply=True))
        self.microphone = ChaCha20Poly1305(session_key(secret, sender, self.session, info=b"SoundFlow-v1-linux-microphone"))
        self.secret = secret
        self.mac_session = None
        self.mac_cipher = None
        self.mac_highest = -1
        self.mac_seen = set()
        self.sequence = 0

    def seal(self, kind: int, payload: bytes) -> tuple:
        if kind not in (HELLO, AUDIO, LINUX_MICROPHONE) or len(payload) > 1300:
            raise ValueError("Invalid packet")
        seq = self.sequence
        self.sequence += 1
        header = HEADER.pack(b"SF01", kind, self.sender.bytes, self.session, seq)
        nonce = b"\x00" * 4 + struct.pack("!Q", seq)
        cipher = self.microphone if kind == LINUX_MICROPHONE else self.client
        return header + cipher.encrypt(nonce, payload, header), seq

    def set_mac_session(self, session: bytes):
        if len(session) != 8:
            raise ValueError("Invalid server session")
        if session != self.mac_session:
            self.mac_session = session
            self.mac_cipher = ChaCha20Poly1305(session_key(self.secret, self.sender, session, info=b"SoundFlow-v1-mac-microphone"))
            self.mac_seen.clear()
            self.mac_highest = -1

    def open_mac_microphone(self, data: bytes) -> Packet:
        if not self.mac_cipher or len(data) != HEADER.size + 16 + 8 + PCM_BYTES:
            raise ValueError("Invalid microphone packet")
        magic, kind, sender, session, seq = HEADER.unpack_from(data)
        if magic != b"SF01" or kind != MAC_MICROPHONE or sender != self.sender.bytes or session != self.mac_session:
            raise ValueError("Invalid microphone identity")
        payload = self.mac_cipher.decrypt(b"\0" * 4 + struct.pack("!Q", seq), data[HEADER.size:], data[:HEADER.size])
        if seq in self.mac_seen or seq <= self.mac_highest - 256:
            raise ValueError("Replayed microphone packet")
        self.mac_seen.add(seq)
        self.mac_highest = max(self.mac_highest, seq)
        self.mac_seen = {value for value in self.mac_seen if value > self.mac_highest - 256}
        return Packet(kind, self.sender, session, seq, payload)

    def open_ack(self, data: bytes) -> Packet:
        if len(data) < HEADER.size + 16 or len(data) > 1400:
            raise ValueError("Invalid packet length")
        magic, kind, sender, session, seq = HEADER.unpack_from(data)
        if magic != b"SF01" or kind != ACK or sender != self.sender.bytes or session != self.session:
            raise ValueError("Invalid acknowledgement")
        payload = self.server.decrypt(b"\x00" * 4 + struct.pack("!Q", seq), data[HEADER.size:], data[:HEADER.size])
        return Packet(kind, self.sender, session, seq, payload)
