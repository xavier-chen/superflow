"""Install/pair/uninstall smoke test in an isolated Linux user's HOME."""
import json
import os
from pathlib import Path
import subprocess

subprocess.run(["pulseaudio", "--start", "--exit-idle-time=-1", "--log-target=file:/tmp/soundflow-pulse.log"], check=True)
key = json.loads(Path("/fixtures/config.json").read_text())["key"]
installed = subprocess.run(["bash", "linux/install.sh", "--host", "host.docker.internal", "--discovery-port", "42890"],
                           input=key + "\n", text=True, capture_output=True, timeout=20)
# Do not print the key or potentially echoed getpass input in diagnostics.
assert installed.returncode == 0, installed.stderr.replace(key, "[test key]")
binary = Path.home() / ".local/bin/soundflow"
config = Path.home() / ".config/soundflow/config.json"
assert binary.is_file() and config.is_file()
assert config.stat().st_mode & 0o777 == 0o600
subprocess.run([str(binary), "doctor"], check=True)
removed = subprocess.run(["bash", "linux/uninstall.sh"], check=True)
assert not binary.exists() and config.exists()
print(f"INSTALL_PAIR_UNINSTALL_PASS arch={os.uname().machine}")
