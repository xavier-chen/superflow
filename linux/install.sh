#!/usr/bin/env bash
set -euo pipefail
if [[ "$(uname -s)" != Linux ]]; then
  echo "此安装脚本用于 Linux；Mac 请打开 SoundFlow.app。" >&2
  exit 1
fi
if [[ "$(id -u)" == 0 ]]; then
  echo "请用桌面登录用户运行，不要 sudo 整个脚本。依赖安装时会单独请求 sudo。" >&2
  exit 1
fi
source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
install_dir="${XDG_DATA_HOME:-$HOME/.local/share}/soundflow"
bin_dir="$HOME/.local/bin"
unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
export PATH="$install_dir/runtime/usr/bin:$PATH"
if ! /usr/bin/python3 -c 'import cryptography' 2>/dev/null || ! command -v parec >/dev/null || ! command -v pactl >/dev/null; then
  if command -v apt-get >/dev/null; then
    sudo apt-get update
    sudo apt-get install -y python3 python3-cryptography pulseaudio-utils
  elif command -v dnf >/dev/null; then
    sudo dnf install -y python3 python3-cryptography pulseaudio-utils
  elif command -v pacman >/dev/null; then
    sudo pacman -S --needed --noconfirm python python-cryptography libpulse
  else
    echo "请先安装 Python 3.9+、python3-cryptography、pactl 和 parec。" >&2
    exit 1
  fi
fi
/usr/bin/python3 -c 'import sys; assert sys.version_info >= (3,9), "需要 Python 3.9+"'
if command -v systemctl >/dev/null; then
  systemctl --user stop soundflow.service 2>/dev/null || true
fi
mkdir -p "$install_dir" "$bin_dir" "$unit_dir"
cp -R "$source_dir/soundflow" "$install_dir/"
# Quote paths using Python's shell quoting rather than expanding user data as shell code.
/usr/bin/python3 - "$install_dir" "$bin_dir/soundflow" <<'PY'
import pathlib, shlex, sys
root, script = sys.argv[1:]
pathlib.Path(script).write_text('#!/bin/sh\nexport PYTHONPATH=' + shlex.quote(root) + '\nexport PATH=' + shlex.quote(root + '/runtime/usr/bin') + ':"$PATH"\nexec /usr/bin/python3 -m soundflow "$@"\n')
pathlib.Path(script).chmod(0o755)
PY
"$bin_dir/soundflow" doctor
if [[ ! -f "${XDG_CONFIG_HOME:-$HOME/.config}/soundflow/config.json" ]]; then
  "$bin_dir/soundflow" pair "$@"
elif [[ $# -gt 0 ]]; then
  "$bin_dir/soundflow" pair "$@"
fi
if command -v systemctl >/dev/null && systemctl --user show-environment >/dev/null 2>&1; then
  # systemd expands %h; quoted executable paths handle spaces in user directories.
  cat > "$unit_dir/soundflow.service" <<EOF
[Unit]
Description=SoundFlow LAN audio sender
After=pipewire-pulse.service pulseaudio.service

[Service]
Type=simple
KillMode=mixed
ExecStart="$bin_dir/soundflow" run
ExecStopPost="$bin_dir/soundflow" restore
Restart=on-failure
RestartSec=3
TimeoutStopSec=10

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable --now soundflow.service
  echo "SoundFlow 已安装，将随用户登录自动连接 Mac。日志：journalctl --user -u soundflow -f"
else
  echo "SoundFlow 已安装。此环境没有 systemd 用户服务，请运行：$bin_dir/soundflow run"
fi
