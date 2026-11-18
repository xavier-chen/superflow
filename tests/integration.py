#!/usr/bin/env python3
"""Runs real Swift/Python encrypted audio, and optional Linux PulseAudio routing."""
import argparse
import array
import json
import math
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import time
import uuid
import wave

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "linux"))
from soundflow.protocol import connection_code

# Public deterministic fixture key. Never use this outside tests.
KEY = connection_code(bytes(range(32)))


def inspect_wav(path, frequencies=()):
    with wave.open(str(path)) as source:
        assert source.getframerate() == 48000 and source.getnchannels() == 2
        values = array.array("h", source.readframes(source.getnframes()))
    nonzero = sum(abs(value) > 200 for value in values)
    rms = math.sqrt(sum(value * value for value in values) / max(1, len(values)))
    assert nonzero > 48000, f"Too little audio: {nonzero} samples, RMS {rms}"
    assert 300 < rms < 20000, f"Unexpected amplitude: RMS {rms}"
    metrics = {"nonzero_samples": nonzero, "rms": round(rms, 2)}
    if frequencies:
        # A steady one-second section must contain each sender's distinct tone.
        section = values[4 * 48000 * 2:5 * 48000 * 2:2]
        amplitudes = {}
        for frequency in frequencies:
            real = sum(value * math.cos(2 * math.pi * frequency * i / 48000) for i, value in enumerate(section))
            imag = sum(value * math.sin(2 * math.pi * frequency * i / 48000) for i, value in enumerate(section))
            magnitude = 2 * math.hypot(real, imag) / len(section)
            assert magnitude > 1500, f"Missing {frequency} Hz source: {magnitude}"
            amplitudes[str(frequency)] = round(magnitude, 2)
        metrics["source_tone_amplitudes"] = amplitudes
    return metrics


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--docker-image", help="Also test a real Linux PulseAudio capture, e.g. soundflow-test:arm64")
    parser.add_argument("--install", action="store_true", help="Test Linux install, interactive pairing and uninstall")
    parser.add_argument("--pipewire", action="store_true", help="Use a PipeWire-Pulse container instead of PulseAudio")
    parser.add_argument("--duplex", action="store_true", help="Verify microphones in both directions using synthetic audio")
    args = parser.parse_args()
    probe = ROOT / ".build/debug/soundflow-probe"
    if not probe.exists():
        raise SystemExit("Run swift build first")
    result_dir = ROOT / ".test-results"
    result_dir.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="soundflow-") as raw:
        temp = Path(raw)
        temp.chmod(0o755)
        key_file = temp / "key.txt"
        key_file.write_text(KEY)
        identity = str(uuid.uuid4())
        receiver_log = temp / "receiver.log"
        suffix = args.docker_image.rsplit(":", 1)[-1] if args.docker_image else "local"
        label = "duplex" if args.duplex else "install" if args.install else "linux-capture" if args.docker_image else "multi-source"
        record = result_dir / f"{label}-{suffix}.wav"
        duration = 18 if args.duplex else 16 if args.docker_image else 10
        with receiver_log.open("w+") as log:
            receiver = subprocess.Popen([str(probe), "--key-file", str(key_file), "--record", str(record),
                                         "--id", identity, "--duration", str(duration),
                                         "--discovery-port", "42890", "--audio-port", "42891"] + (["--duplex-test"] if args.duplex else []), stdout=log, stderr=subprocess.STDOUT)
            clients = []
            try:
                deadline = time.monotonic() + 5
                while time.monotonic() < deadline:
                    if "READY" in receiver_log.read_text():
                        break
                    if receiver.poll() is not None:
                        raise RuntimeError(receiver_log.read_text())
                    time.sleep(0.05)
                else:
                    raise RuntimeError("Receiver did not become ready")
                if args.docker_image:
                    config = {"key": KEY, "receiver_id": identity, "sender_id": str(uuid.uuid4()), "name": "Linux real capture"}
                    (temp / "config.json").write_text(json.dumps(config))
                    command = ["docker", "run", "--rm", "-v", f"{ROOT}:/work:ro", "-v", f"{temp}:/fixtures:ro"]
                    if args.pipewire:
                        command += ["-e", "SOUNDFLOW_TEST_PIPEWIRE=1"]
                    command += [args.docker_image]
                    if args.pipewire:
                        command += ["dbus-run-session", "--"]
                    command += ["python3", "tests/linux_duplex.py" if args.duplex else "tests/linux_install.py" if args.install else "tests/linux_capture.py"]
                    clients.append(subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True))
                else:
                    for index in range(2):
                        config = temp / f"sender{index}.json"
                        config.write_text(json.dumps({"key": KEY, "receiver_id": identity,
                                                      "sender_id": str(uuid.uuid4()), "name": f"Test sender {index}", "test_frequency": 440 + 220 * index}))
                        env = {**os.environ, "PYTHONPATH": str(ROOT / "linux"), "XDG_STATE_HOME": str(temp / f"state{index}")}
                        clients.append(subprocess.Popen([sys.executable, "-m", "soundflow", "--config", str(config), "run",
                                                         "--test-tone", "--duration", "7", "--discovery-port", "42890"],
                                                        env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True))
                client_logs = []
                for process in clients:
                    output, _ = process.communicate(timeout=25)
                    client_logs.append(output)
                    assert process.returncode == 0, output
                receiver.wait(timeout=duration + 5)
                assert receiver.returncode == 0, receiver_log.read_text()
                text = receiver_log.read_text()
                expected_peers = 1 if args.docker_image else 2
                assert text.count("PEER ") == expected_peers, text
                metrics = {} if args.install else inspect_wav(record, () if args.docker_image else (440, 660))
                report = {"mode": "bidirectional-microphones" if args.duplex else "install-pair-uninstall" if args.install else "linux-pulse-capture" if args.docker_image else "two-senders-auto-discovery",
                          "image": args.docker_image, "metrics": metrics, "receiver": text, "clients": client_logs}
                destination = result_dir / f"{label}-{suffix}.json"
                destination.write_text(json.dumps(report, ensure_ascii=False, indent=2))
                print(json.dumps({"result": "PASS", "mode": report["mode"], "metrics": metrics}, ensure_ascii=False))
                print(text.strip())
            finally:
                for process in clients:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            process.kill()
                if receiver.poll() is None:
                    receiver.terminate(); receiver.wait(timeout=5)


if __name__ == "__main__":
    main()
