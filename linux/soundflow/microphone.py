"""Separate physical-microphone capture and a virtual Mac microphone; no speaker loopback."""
import json
import os
import subprocess
import time
from pathlib import Path
from .audio import pactl, private_json
from .protocol import PCM_BYTES

BUS = "soundflow_microphone_bus"
SOURCE = "soundflow_microphone"


def physical_microphones():
    sources = json.loads(pactl("--format=json", "list", "sources"))
    available = []
    for source in sources:
        if source["name"].endswith(".monitor") or source["name"].startswith("soundflow_"):
            continue
        ports = source.get("ports", [])
        states = [str(port.get("availability", port.get("available", "unknown"))).lower().replace("_", " ") for port in ports]
        if states and all(state in ("not available", "no", "false") for state in states):
            continue
        available.append({"name": source["name"][:160], "description": source.get("description", source["name"])[:60]})
    return available[:4]


class PhysicalMicrophone:
    def __init__(self):
        self.process = None
        self.source = None

    def start(self, source=""):
        available = physical_microphones()
        if not available:
            raise RuntimeError("Linux 未检测到已接入的物理麦克风，请插入或连接麦克风")
        names = [item["name"] for item in available]
        selected = source or (pactl("get-default-source") if pactl("get-default-source") in names else names[0])
        if selected not in names:
            raise RuntimeError("所选 Linux 麦克风不可用")
        self.stop()
        self.process = subprocess.Popen(["parec", "--raw", f"--device={selected}", "--format=s16le",
            "--rate=48000", "--channels=2", "--channel-map=front-left,front-right", "--latency-msec=20",
            "--process-time-msec=5", "--client-name=SoundFlow", "--stream-name=SoundFlow physical microphone"],
            stdout=subprocess.PIPE, stderr=None, bufsize=0)
        os.set_blocking(self.process.stdout.fileno(), False)
        self.source = selected
        return self.process.stdout

    def stop(self):
        if self.process:
            self.process.terminate()
            try: self.process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.process.kill(); self.process.wait()
            self.process.stdout.close()
        self.process = None; self.source = None


class MacMicrophone:
    def __init__(self, state_path: Path):
        self.state_path = state_path
        self.process = None
        self.pending = {}
        self.expected = None
        self.first = 0.0
        self.next_play = 0.0
        self.bytes = bytearray()

    def start(self):
        if self.process: return
        self.restore()
        original = pactl("get-default-source")
        private_json(self.state_path, {"original": original, "source": SOURCE})
        try:
            pactl("load-module", "module-null-sink", f"sink_name={BUS}", "rate=48000", "channels=2",
                  "sink_properties=device.description=SoundFlow-Microphone-Bus")
            pactl("load-module", "module-remap-source", f"master={BUS}.monitor", f"source_name={SOURCE}",
                  "source_properties=device.description=SoundFlow-Mac-Microphone")
            pactl("set-default-source", SOURCE)
            self.process = subprocess.Popen(["paplay", "--raw", f"--device={BUS}", "--format=s16le", "--rate=48000",
                "--channels=2", "--channel-map=front-left,front-right", "--latency-msec=30", "--client-name=SoundFlow",
                "--stream-name=SoundFlow Mac microphone"], stdin=subprocess.PIPE, bufsize=0)
            os.set_blocking(self.process.stdin.fileno(), False)
        except Exception:
            self.stop(); raise

    def feed(self, frame, pcm, now):
        if len(pcm) != PCM_BYTES or frame % 240 or (self.expected is not None and frame < self.expected): return
        if len(self.pending) >= 48:
            self.expected = min(self.pending)
            self.pending.pop(self.expected)
            self.expected += 240
        self.pending[frame] = pcm
        if not self.first: self.first = now

    def pump(self, now):
        if not self.process or not self.first or now - self.first < .035: return
        if self.process.poll() is not None: raise RuntimeError("Linux 虚拟麦克风播放进程已退出")
        if self.expected is None: self.expected = min(self.pending)
        if now >= self.next_play:
            if len(self.pending) > 24: self.expected = sorted(self.pending)[-12]
            pcm = self.pending.pop(self.expected, bytes(PCM_BYTES))
            self.pending = {frame: data for frame, data in self.pending.items() if frame > self.expected}
            self.expected += 240
            if len(self.bytes) > PCM_BYTES * 8: self.bytes.clear()
            self.bytes.extend(pcm)
            self.next_play = max(self.next_play + .005, now)
        if self.bytes:
            try:
                written = os.write(self.process.stdin.fileno(), self.bytes)
                del self.bytes[:written]
            except BlockingIOError: pass

    def restore(self):
        if not self.state_path.exists(): return
        state = json.loads(self.state_path.read_text())
        if state.get("source") != SOURCE: raise RuntimeError("无效的麦克风恢复状态")
        sources = [line.split() for line in pactl("list", "short", "sources").splitlines()]
        names = [row[1] for row in sources if len(row) > 1 and not row[1].startswith("soundflow_")]
        fallback = state.get("original") if state.get("original") in names else next(iter(names), None)
        current = pactl("get-default-source")
        if current == SOURCE and fallback: pactl("set-default-source", fallback)
        owned = next((row[0] for row in sources if len(row) > 1 and row[1] == SOURCE), None)
        if owned and fallback:
            for line in pactl("list", "short", "source-outputs").splitlines():
                row = line.split()
                if len(row) > 1 and row[1] == owned: pactl("move-source-output", row[0], fallback, check=False)
        modules = [line.split() for line in pactl("list", "short", "modules").splitlines()]
        for kind, marker in [("module-remap-source", f"source_name={SOURCE}"), ("module-null-sink", f"sink_name={BUS}")]:
            for row in modules:
                if len(row) > 2 and row[1] == kind and marker in row[2:]: pactl("unload-module", row[0])
        self.state_path.unlink(missing_ok=True)

    def stop(self):
        if self.process:
            self.process.terminate()
            try: self.process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.process.kill(); self.process.wait()
            self.process.stdin.close()
        self.process = None; self.pending.clear(); self.bytes.clear(); self.expected = None; self.first = 0; self.next_play = 0
        self.restore()
