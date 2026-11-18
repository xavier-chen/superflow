"""Inside Linux container: actual PulseAudio -> parec -> UDP -> Mac, then restore."""
import array
import json
import math
import os
import shutil
import signal
import subprocess
import time
import wave
from pathlib import Path

def command(*args):
    return subprocess.check_output(args, text=True).strip()

root = Path("/tmp/soundflow-capture")
root.mkdir()
shutil.copy("/fixtures/config.json", root / "config.json")
os.environ["XDG_STATE_HOME"] = str(root / "state")
daemons = []
if os.environ.get("SOUNDFLOW_TEST_PIPEWIRE") == "1":
    runtime = root / "runtime"
    runtime.mkdir(mode=0o700)
    os.environ["XDG_RUNTIME_DIR"] = str(runtime)
    for name in ("pipewire", "pipewire-pulse", "wireplumber"):
        daemons.append(subprocess.Popen([name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if subprocess.run(["pactl", "info"], capture_output=True).returncode == 0:
            break
        time.sleep(.1)
else:
    subprocess.run(["pulseaudio", "--start", "--exit-idle-time=-1", "--log-target=file:/tmp/soundflow-pulse.log"], check=True)
command("pactl", "load-module", "module-null-sink", "sink_name=local_speakers", "rate=48000", "channels=2")
deadline = time.monotonic() + 5
while True:
    # pipewire-pulse can accept connections before WirePlumber creates default metadata.
    result = subprocess.run(["pactl", "set-default-sink", "local_speakers"], capture_output=True)
    if result.returncode == 0:
        break
    if time.monotonic() >= deadline:
        raise RuntimeError("Session manager did not become ready: " + result.stderr.decode())
    time.sleep(.1)
original = command("pactl", "get-default-sink")
tone = root / "tone.wav"
with wave.open(str(tone), "wb") as output:
    output.setparams((2, 2, 48000, 0, "NONE", "not compressed"))
    samples = array.array("h")
    for i in range(48000 * 10):
        value = int(9000 * math.sin(2 * math.pi * 523.25 * i / 48000))
        samples.extend((value, value))
    output.writeframes(samples.tobytes())
sender = subprocess.Popen(["python3", "-m", "soundflow", "--config", str(root / "config.json"), "run",
                           "--host", "host.docker.internal", "--discovery-port", "42890"],
                          stdout=None, stderr=None, start_new_session=True)
play = None
try:
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        if command("pactl", "get-default-sink") == "soundflow_output":
            break
        if sender.poll() is not None:
            raise RuntimeError("Sender exited before audio routing")
        time.sleep(0.1)
    else:
        raise RuntimeError("No automatic audio routing")
    play = subprocess.Popen(["paplay", str(tone)])
    time.sleep(5)
    # Match a service manager stopping the sender and capture child together.
    os.killpg(sender.pid, signal.SIGTERM)
    sender.wait(timeout=8)
    assert sender.returncode == 0
    restored = command("pactl", "get-default-sink")
    assert restored == original, f"Audio route not restored: {restored}"
    sinks = command("pactl", "list", "short", "sinks")
    assert "soundflow_output" not in sinks
    print(f"CAPTURE_AND_RESTORE_PASS arch={os.uname().machine} original={original} restored={restored}")
finally:
    if play and play.poll() is None:
        play.terminate(); play.wait(timeout=3)
    if sender.poll() is None:
        sender.terminate(); sender.wait(timeout=8)
    for daemon in reversed(daemons):
        daemon.terminate(); daemon.wait(timeout=3)
