import json
import logging
import math
import os
import platform
import select
import socket
import struct
import subprocess
import time
import uuid
from cryptography.exceptions import InvalidTag

from .audio import AudioRoute
from .discovery import discover
from .protocol import AUDIO, CHANNELS, FRAMES, HELLO, PCM_BYTES, RATE, Cipher, parse_code, MAC_MICROPHONE, LINUX_MICROPHONE
from .microphone import PhysicalMicrophone, MacMicrophone, physical_microphones

LOG = logging.getLogger("soundflow")


class Sender:
    def __init__(self, config, state_path, *, host=None, discovery_port=42790, monitor_only=False,
                 test_tone=False, duration=None):
        self.config, self.state_path = config, state_path
        self.host = host or config.get("host")
        self.discovery_port = discovery_port
        self.route = AudioRoute(state_path, monitor_only)
        self.test_tone, self.duration = test_tone, duration
        self.stopping = False
        self.cipher = Cipher(parse_code(config["key"]), uuid.UUID(config["sender_id"]))
        self.name = str(config.get("name") or socket.gethostname())[:80]
        self.physical_mic = PhysicalMicrophone()
        self.mac_mic = MacMicrophone(state_path.with_name("microphone-route.json"))
        self.microphones = []
        self.microphone_status = ""
        self.last_source_scan = -10.0

    def stop(self, *_):
        self.stopping = True

    def hello(self, receiver):
        now = time.monotonic()
        if now - self.last_source_scan > 5:
            try: self.microphones = physical_microphones()
            except (OSError, RuntimeError, ValueError): self.microphones = []
            self.last_source_scan = now
        body = {"name": self.name, "arch": platform.machine(), "platform": "Linux",
                           "rate": RATE, "channels": CHANNELS, "format": "s16le",
                           "receiver": receiver.id, "challenge": receiver.challenge,
                           "capabilities": ["microphone-v1"], "microphones": list(self.microphones),
                           "microphone_status": self.microphone_status[:100]}
        while len(json.dumps(body).encode()) > 1200 and body["microphones"]: body["microphones"].pop()
        return json.dumps(body, separators=(",", ":")).encode()

    def run(self):
        started = time.monotonic()
        if not self.test_tone:
            self.route.check_dependencies()
            if not self.route.restore():
                raise RuntimeError("无法恢复上次声音路由，暂不接管新的声音。")
            self.mac_mic.restore()
        try:
            while not self.stopping and (self.duration is None or time.monotonic() - started < self.duration):
                LOG.info("正在发现局域网中的 Mac…")
                receivers = discover(timeout=1.5, host=self.host, port=self.discovery_port)
                receiver = next((item for item in receivers if item.id == self.config["receiver_id"]), None)
                if not receiver:
                    continue
                LOG.info("发现 %s (%s)，正在验证连接…", receiver.name, receiver.host)
                self._stream(receiver, started)
        finally:
            if not self.test_tone:
                self.route.stop()

    def authenticate(self, receiver, timeout=3):
        """Used during setup; does not capture or redirect any audio."""
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.settimeout(timeout)
            packet, seq = self.cipher.seal(HELLO, self.hello(receiver))
            sock.sendto(packet, (receiver.host, receiver.port))
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                data, address = sock.recvfrom(2048)
                if address != (receiver.host, receiver.port):
                    continue
                try:
                    ack = self.cipher.open_ack(data)
                    identity = json.loads(ack.payload)["id"]
                    if ack.sequence == seq and identity == receiver.id:
                        return True
                except (ValueError, KeyError, InvalidTag):
                    continue
            return False

    def _stream(self, receiver, started):
        self.cipher = Cipher(parse_code(self.config["key"]), uuid.UUID(self.config["sender_id"]))
        capture = None
        microphone_capture = None
        microphone_buffer = bytearray()
        microphone_frame = 0
        last_ack = 0.0
        next_hello, next_tone = 0.0, 0.0
        outstanding = {}
        frame = 0
        buffered = bytearray()
        address = (receiver.host, receiver.port)
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.bind(("0.0.0.0", 0))
            sock.setblocking(False)
            connected_since = time.monotonic()
            try:
                while not self.stopping:
                    now = time.monotonic()
                    if self.duration is not None and now - started >= self.duration:
                        self.stopping = True
                        break
                    if now >= next_hello:
                        packet, seq = self.cipher.seal(HELLO, self.hello(receiver))
                        sock.sendto(packet, address)
                        outstanding[seq] = now
                        outstanding = {seq: sent for seq, sent in outstanding.items() if now - sent < 3}
                        next_hello = now + 0.8
                    if now - (last_ack or connected_since) > 3:
                        LOG.warning("Mac 已断开，恢复本地声音并重新发现。")
                        break
                    timeout = max(0.0, min(0.02, next_hello - now))
                    if self.test_tone and last_ack:
                        timeout = min(timeout, max(0.0, next_tone - now))
                    if self.mac_mic.process: timeout = min(timeout, .005)
                    ready, _, _ = select.select([sock] + ([capture] if capture else []) + ([microphone_capture] if microphone_capture else []), [], [], timeout)
                    if self.stopping:
                        break
                    if sock in ready:
                        for _ in range(32):
                            try:
                                data, source = sock.recvfrom(2048)
                            except BlockingIOError:
                                break
                            if source != address:
                                continue
                            try:
                                if len(data) > 4 and data[4] == MAC_MICROPHONE:
                                    if self.mac_mic.process:
                                        incoming = self.cipher.open_mac_microphone(data)
                                        self.mac_mic.feed(struct.unpack_from("!Q", incoming.payload)[0], incoming.payload[8:], time.monotonic())
                                    continue
                                ack = self.cipher.open_ack(data)
                                body = json.loads(ack.payload)
                                if ack.sequence not in outstanding or body.get("id") != self.config["receiver_id"]:
                                    continue
                                del outstanding[ack.sequence]
                                first = not last_ack
                                last_ack = time.monotonic()
                                if first:
                                    LOG.info("已连接 %s，%s", receiver.name, "发送测试音" if self.test_tone else "正在接管系统声音")
                                    next_tone = last_ack
                                    if not self.test_tone:
                                        capture = self.route.start()
                                if body.get("server_session"):
                                    self.cipher.set_mac_session(bytes.fromhex(body["server_session"]))
                                if not self.test_tone:
                                    microphone_errors = []
                                    try:
                                        if body.get("mac_microphone"):
                                            self.mac_mic.start()
                                        elif self.mac_mic.process:
                                            self.mac_mic.stop()
                                    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                                        microphone_errors.append(str(error))
                                    try:
                                        requested_source = body.get("microphone_source", "")
                                        if body.get("request_microphone"):
                                            if microphone_capture is None or (requested_source and requested_source != self.physical_mic.source) or self.physical_mic.source not in {item["name"] for item in self.microphones}:
                                                microphone_capture = self.physical_mic.start(requested_source)
                                                microphone_buffer.clear()
                                        elif microphone_capture:
                                            self.physical_mic.stop(); microphone_capture = None; microphone_buffer.clear()
                                    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                                        self.physical_mic.stop(); microphone_capture = None; microphone_buffer.clear()
                                        microphone_errors.append(str(error))
                                    status = "; ".join(microphone_errors)[:100]
                                    if status and status != self.microphone_status: LOG.warning("麦克风同步：%s", status)
                                    self.microphone_status = status
                            except (ValueError, KeyError, InvalidTag):
                                continue
                    if capture and capture in ready:
                        chunk = os.read(capture.fileno(), PCM_BYTES * 16)
                        if not chunk:
                            if self.stopping:
                                break
                            raise RuntimeError("系统声音采集意外退出。请检查 PipeWire/PulseAudio 用户会话。")
                        buffered.extend(chunk)
                        # Discard stale capture backlog, preserving low latency over lossless delivery.
                        if len(buffered) > PCM_BYTES * 8:
                            del buffered[:((len(buffered) // PCM_BYTES) - 4) * PCM_BYTES]
                        while len(buffered) >= PCM_BYTES:
                            pcm = bytes(buffered[:PCM_BYTES]); del buffered[:PCM_BYTES]
                            packet, _ = self.cipher.seal(AUDIO, struct.pack("!Q", frame) + pcm)
                            sock.sendto(packet, address)
                            frame += FRAMES
                    if microphone_capture and microphone_capture in ready:
                        try: chunk = os.read(microphone_capture.fileno(), PCM_BYTES * 8)
                        except BlockingIOError: chunk = None
                        if chunk == b"":
                            self.physical_mic.stop(); microphone_capture = None
                            self.microphone_status = "Linux 麦克风采集已中断"
                        elif chunk:
                            microphone_buffer.extend(chunk)
                            if len(microphone_buffer) > PCM_BYTES * 8:
                                del microphone_buffer[:((len(microphone_buffer) // PCM_BYTES) - 4) * PCM_BYTES]
                            while len(microphone_buffer) >= PCM_BYTES:
                                pcm = bytes(microphone_buffer[:PCM_BYTES]); del microphone_buffer[:PCM_BYTES]
                                packet, _ = self.cipher.seal(LINUX_MICROPHONE, struct.pack("!Q", microphone_frame) + pcm)
                                sock.sendto(packet, address); microphone_frame += FRAMES
                    if self.mac_mic.process:
                        try: self.mac_mic.pump(time.monotonic())
                        except (OSError, RuntimeError) as error:
                            self.microphone_status = str(error)[:100]; self.mac_mic.stop()
                    if self.test_tone and last_ack and time.monotonic() >= next_tone:
                        pcm = b"".join(struct.pack("<hh", value, value) for value in
                                       (int(7000 * math.sin(2 * math.pi * float(self.config.get("test_frequency", 440)) * (frame + i) / RATE)) for i in range(FRAMES)))
                        packet, _ = self.cipher.seal(AUDIO, struct.pack("!Q", frame) + pcm)
                        sock.sendto(packet, address)
                        frame += FRAMES
                        next_tone = max(next_tone + FRAMES / RATE, time.monotonic() - 0.01)
            finally:
                self.physical_mic.stop()
                try: self.mac_mic.stop()
                except (OSError, RuntimeError): LOG.exception("恢复麦克风路由失败")
                if not self.test_tone:
                    self.route.stop()
