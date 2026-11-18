#!/usr/bin/env bash
set -euo pipefail
systemctl --user disable --now soundflow.service 2>/dev/null || true
if [[ -x "$HOME/.local/bin/soundflow" ]]; then
  "$HOME/.local/bin/soundflow" restore
fi
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/soundflow.service"
systemctl --user daemon-reload 2>/dev/null || true
rm -rf "${XDG_DATA_HOME:-$HOME/.local/share}/soundflow"
rm -f "$HOME/.local/bin/soundflow"
echo "已卸载。连接配置保留在 ${XDG_CONFIG_HOME:-$HOME/.config}/soundflow；可手动删除。"
