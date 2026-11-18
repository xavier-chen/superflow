"""Route ordinary desktop audio through a monitor, restoring it on disconnect."""
import json
import logging
import os
import shutil
import subprocess
from pathlib import Path

LOG = logging.getLogger("soundflow")
SINK = "soundflow_output"


def pactl(*args, check=True):
    result = subprocess.run(["pactl", *map(str, args)], capture_output=True, text=True, timeout=5)
    if check and result.returncode:
        raise RuntimeError(result.stderr.strip() or "无法连接用户的音频服务。请在桌面登录用户下运行。")
    return result.stdout.strip()


def private_json(path: Path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix(".tmp")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w") as handle:
        json.dump(value, handle)
    os.replace(temporary, path)


class AudioRoute:
    def __init__(self, state_path: Path, monitor_only=False):
        self.state_path, self.monitor_only = state_path, monitor_only
        self.capture = None
        self.active = False

    @staticmethod
    def check_dependencies():
        missing = [name for name in ("pactl", "parec") if not shutil.which(name)]
        if missing:
            raise RuntimeError("缺少 " + ", ".join(missing) + "，请运行 Linux 安装脚本安装音频依赖。")
        pactl("info")

    def start(self):
        self.check_dependencies()
        if not self.restore():
            raise RuntimeError("上次的声音路由尚未恢复，请先运行 soundflow restore。")
        original = pactl("get-default-sink")
        if not original or original == SINK:
            raise RuntimeError("未找到可恢复的本地输出设备。")
        if not self.monitor_only and any(len(row) > 1 and row[1] == SINK for row in
                                         (line.split() for line in pactl("list", "short", "sinks").splitlines())):
            raise RuntimeError("已有同名 SoundFlow 输出，请先停止另一个实例或检查音频服务。")
        monitor = original + ".monitor"
        if not self.monitor_only:
            # Write intent before loading the module so interruption is recoverable.
            private_json(self.state_path, {"original": original, "module": None, "sink": SINK})
            try:
                module = pactl("load-module", "module-null-sink", f"sink_name={SINK}",
                               "rate=48000", "channels=2", "channel_map=front-left,front-right",
                               "sink_properties=device.description=SoundFlow")
                private_json(self.state_path, {"original": original, "module": module, "sink": SINK})
                pactl("set-default-sink", SINK)
                for line in pactl("list", "short", "sink-inputs").splitlines():
                    self._move(line.split()[0], SINK)
                monitor = SINK + ".monitor"
            except Exception:
                self.restore()
                raise
        try:
            self.capture = subprocess.Popen([
                "parec", "--raw", f"--device={monitor}", "--format=s16le", "--rate=48000", "--channels=2",
                "--channel-map=front-left,front-right", "--latency-msec=20", "--process-time-msec=5",
                "--client-name=SoundFlow", "--stream-name=SoundFlow system audio",
            ], stdout=subprocess.PIPE, stderr=None, bufsize=0)
            os.set_blocking(self.capture.stdout.fileno(), False)
            self.active = True
            return self.capture.stdout
        except Exception:
            self.stop()
            raise

    @staticmethod
    def _move(index, sink):
        # Streams can disappear between listing and moving; that is harmless.
        pactl("move-sink-input", index, sink, check=False)

    def restore(self):
        if not self.state_path.exists():
            return True
        try:
            state = json.loads(self.state_path.read_text())
            if state.get("sink") != SINK:
                raise RuntimeError("无效的声音路由恢复文件。")
            sinks = [line.split() for line in pactl("list", "short", "sinks").splitlines() if line.strip()]
            owned = next((row for row in sinks if row[1] == SINK), None)
            if owned is None:
                self.state_path.unlink(missing_ok=True)
                return True
            current = pactl("get-default-sink")
            names = [row[1] for row in sinks if row[1] != SINK]
            # Respect a deliberate output change made while SoundFlow was connected.
            fallback = current if current in names else state["original"] if state["original"] in names else next(iter(names), None)
            if not fallback:
                # Headless machines may only have auto_null. Loading our sink can
                # remove it; unloading our sink lets the server recreate it.
                self._unload_owned_modules()
                self.state_path.unlink(missing_ok=True)
                return True
            if current == SINK:
                pactl("set-default-sink", fallback)
            for line in pactl("list", "short", "sink-inputs").splitlines():
                row = line.split()
                if len(row) > 1 and row[1] == owned[0]:
                    self._move(row[0], fallback)
            # Verify the owner, rather than unloading a possibly recycled numeric module ID.
            self._unload_owned_modules()
            self.state_path.unlink(missing_ok=True)
            return True
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError, KeyError, IndexError) as exc:
            LOG.warning("恢复本地声音失败，将保留恢复文件并重试：%s", exc)
            return False

    @staticmethod
    def _unload_owned_modules():
        for line in pactl("list", "short", "modules").splitlines():
            row = line.split()
            if len(row) >= 3 and row[1] == "module-null-sink" and f"sink_name={SINK}" in row[2:]:
                pactl("unload-module", row[0])

    def stop(self):
        if self.capture:
            self.capture.terminate()
            try:
                self.capture.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.capture.kill()
                self.capture.wait()
            if self.capture.stdout:
                self.capture.stdout.close()
            self.capture = None
        self.active = False
        return self.restore()
