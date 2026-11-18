"""Linux half of the network-disconnect/reconnect integration test."""
import os
import shutil
import signal
import subprocess
import time
from pathlib import Path

def command(*args):
    return subprocess.check_output(args, text=True).strip()

root = Path("/tmp/soundflow-reconnect")
root.mkdir()
shutil.copy("/fixtures/config.json", root / "config.json")
os.environ["XDG_STATE_HOME"] = str(root / "state")
subprocess.run(["pulseaudio", "--start", "--exit-idle-time=-1", "--log-target=file:/tmp/soundflow-pulse.log"], check=True)
command("pactl", "load-module", "module-null-sink", "sink_name=local_speakers", "rate=48000", "channels=2")
command("pactl", "set-default-sink", "local_speakers")
sender = subprocess.Popen(["python3", "-m", "soundflow", "--config", str(root / "config.json"), "run",
                           "--host", "host.docker.internal", "--discovery-port", "42990"])

def wait_sink(expected, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if command("pactl", "get-default-sink") == expected:
            return
        if sender.poll() is not None:
            raise RuntimeError("Sender unexpectedly exited")
        time.sleep(0.1)
    raise RuntimeError(f"Timeout waiting for sink {expected}")

try:
    wait_sink("soundflow_output", 6)
    print("FIRST_CONNECTION_OK", flush=True)
    wait_sink("local_speakers", 12)
    print("DISCONNECTED_AND_RESTORED_OK", flush=True)
    wait_sink("soundflow_output", 10)
    print("RECONNECTED_OK", flush=True)
    sender.send_signal(signal.SIGTERM)
    sender.wait(timeout=8)
    assert sender.returncode == 0
    assert command("pactl", "get-default-sink") == "local_speakers"
    print("FINAL_RESTORE_OK", flush=True)
finally:
    if sender.poll() is None:
        sender.terminate(); sender.wait(timeout=8)
