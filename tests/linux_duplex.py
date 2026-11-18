"""Synthetic microphones only: validate both real PulseAudio virtual-device paths."""
import array
import math
import os
import shutil
import signal
import subprocess
import time
import wave
from pathlib import Path

def command(*args): return subprocess.check_output(args, text=True).strip()
root = Path("/tmp/soundflow-duplex")
root.mkdir()
shutil.copy("/fixtures/config.json", root / "config.json")
os.environ["XDG_STATE_HOME"] = str(root / "state")
subprocess.run(["pulseaudio", "--start", "--exit-idle-time=-1", "--log-target=file:/tmp/soundflow-pulse.log"], check=True)
for name in ["local_speakers", "test_microphone_bus"]:
    command("pactl", "load-module", "module-null-sink", f"sink_name={name}", "rate=48000", "channels=2")
command("pactl", "load-module", "module-remap-source", "master=test_microphone_bus.monitor", "source_name=test_physical_microphone")
command("pactl", "set-default-sink", "local_speakers")
command("pactl", "set-default-source", "test_physical_microphone")
tone = root / "physical.wav"
with wave.open(str(tone), "wb") as output:
    output.setparams((2, 2, 48000, 0, "NONE", "not compressed"))
    samples = array.array("h")
    for i in range(48000 * 7):
        value = int(8000 * math.sin(2 * math.pi * 523.25 * i / 48000))
        samples.extend((value, value))
    output.writeframes(samples.tobytes())
sender = subprocess.Popen(["python3", "-m", "soundflow", "--config", str(root / "config.json"), "run", "--host", "host.docker.internal", "--discovery-port", "42890"], start_new_session=True)
capture = play = None
try:
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if command("pactl", "get-default-source") == "soundflow_microphone": break
        if sender.poll() is not None: raise RuntimeError("Sender exited before virtual microphone was ready")
        time.sleep(.1)
    else: raise RuntimeError("Mac microphone did not become the Linux default input")
    play = subprocess.Popen(["paplay", "--device=test_microphone_bus", str(tone)])
    record = open(root / "mac-microphone.raw", "wb")
    capture = subprocess.Popen(["parec", "--raw", "--device=soundflow_microphone", "--format=s16le", "--rate=48000", "--channels=2", "--latency-msec=20"], stdout=record)
    time.sleep(4)
    capture.terminate(); capture.wait(timeout=3); record.close(); capture = None
    values = array.array("h", (root / "mac-microphone.raw").read_bytes())[::2]
    section = values[48000:96000]
    assert len(section) == 48000, f"Short Mac microphone capture: {len(values)}"
    # Tolerate resampling drift in the virtual sink while requiring the correct stream.
    rms = math.sqrt(sum(v * v for v in section) / len(section))
    crossings = sum(a <= 0 < b for a, b in zip(section, section[1:]))
    assert rms > 1000, f"Mac microphone is silent: RMS={rms}"
    assert 630 < crossings < 690, f"Wrong Mac microphone signal: {crossings} crossings"
    os.killpg(sender.pid, signal.SIGTERM); sender.wait(timeout=8)
    assert sender.returncode == 0
    assert command("pactl", "get-default-source") == "test_physical_microphone"
    assert command("pactl", "get-default-sink") == "local_speakers"
    assert "soundflow_microphone" not in command("pactl", "list", "short", "sources")
    print(f"DUPLEX_AND_RESTORE_PASS arch={os.uname().machine} mac_mic_rms={rms:.2f} mac_mic_frequency={crossings}Hz")
finally:
    for process in [capture, play, sender]:
        if process and process.poll() is None:
            process.terminate(); process.wait(timeout=8)
