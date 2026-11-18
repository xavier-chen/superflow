#!/usr/bin/env python3
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "linux"))
from soundflow.protocol import connection_code

image = sys.argv[1] if len(sys.argv) > 1 else "soundflow-test:arm64"
with tempfile.TemporaryDirectory(prefix="soundflow-reconnect-") as path:
    temp = Path(path); temp.chmod(0o755)
    key = connection_code(bytes(range(32)))
    (temp / "key.txt").write_text(key)
    identity = str(uuid.uuid4())
    (temp / "config.json").write_text(json.dumps({"key": key, "receiver_id": identity,
                                                  "sender_id": str(uuid.uuid4()), "name": "Reconnect test Linux"}))
    probes = []
    def start_probe(number, duration):
        log = (temp / f"probe{number}.log").open("w")
        process = subprocess.Popen([str(ROOT / ".build/debug/soundflow-probe"), "--key-file", str(temp / "key.txt"),
                                    "--record", str(temp / f"capture{number}.wav"), "--id", identity,
                                    "--duration", str(duration), "--discovery-port", "42990", "--audio-port", "42991"],
                                   stdout=log, stderr=subprocess.STDOUT)
        probes.append((process, log))
        deadline = time.monotonic() + 3
        while "READY" not in (temp / f"probe{number}.log").read_text():
            if process.poll() is not None or time.monotonic() > deadline:
                raise RuntimeError((temp / f"probe{number}.log").read_text())
            time.sleep(.05)
    client = None
    try:
        start_probe(1, 7)
        client = subprocess.Popen(["docker", "run", "--rm", "-v", f"{ROOT}:/work:ro", "-v", f"{temp}:/fixtures:ro",
                                   image, "python3", "tests/linux_reconnect.py"],
                                  stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        lines = []
        # The Linux side has bounded timeouts at each checkpoint.
        for line in client.stdout:
            lines.append(line); print(line, end="", flush=True)
            if "DISCONNECTED_AND_RESTORED_OK" in line:
                probes[0][0].wait(timeout=2)
                start_probe(2, 7)
        client.wait(timeout=5)
        assert client.returncode == 0, "".join(lines)
        assert all(marker in "".join(lines) for marker in ("FIRST_CONNECTION_OK", "DISCONNECTED_AND_RESTORED_OK", "RECONNECTED_OK", "FINAL_RESTORE_OK"))
        result = ROOT / ".test-results/reconnect.json"
        result.parent.mkdir(exist_ok=True)
        result.write_text(json.dumps({"result": "PASS", "image": image, "log": "".join(lines)}, ensure_ascii=False, indent=2))
        print("NETWORK_RECONNECT_PASS")
    finally:
        if client and client.poll() is None:
            client.terminate()
        for process, log in probes:
            if process.poll() is None:
                process.terminate()
            process.wait(timeout=5); log.close()
