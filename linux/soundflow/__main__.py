import argparse
import fcntl
import getpass
import json
import logging
import os
import signal
import socket
import subprocess
import sys
import uuid
from pathlib import Path

from . import __version__
from .audio import AudioRoute, private_json
from .discovery import discover
from .protocol import parse_code
from .sender import Sender
from .microphone import MacMicrophone


def paths():
    config = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config")) / "soundflow"
    state = Path(os.environ.get("XDG_STATE_HOME", Path.home() / ".local/state")) / "soundflow"
    return config / "config.json", state / "route.json"


def pair(args, config_path, state_path):
    code = getpass.getpass("粘贴 Mac 上的连接密钥（输入不会显示）：").strip()
    parse_code(code)
    print("正在发现局域网中的 SoundFlow Mac…")
    receivers = discover(timeout=2.5, host=args.host, port=args.discovery_port)
    if not receivers:
        raise RuntimeError("未发现 Mac。请打开 Mac 上的 SoundFlow，检查两台设备在同一局域网，或用 --host 指定 Mac IP。")
    existing = json.loads(config_path.read_text()) if config_path.exists() else {}
    config = {"key": code, "sender_id": existing.get("sender_id", str(uuid.uuid4())), "name": args.name or socket.gethostname()}
    # Verify the key against each discovered receiver; no arbitrary 'first Mac wins'.
    for receiver in receivers:
        config["receiver_id"] = receiver.id
        sender = Sender(config, state_path)
        try:
            if sender.authenticate(receiver):
                if args.host:
                    config["host"] = args.host
                private_json(config_path, config)
                print(f"已配对 {receiver.name}。以后会自动发现并重连。")
                return
        except (OSError, TimeoutError):
            continue
    raise RuntimeError("连接密钥未通过验证。请检查是否复制了正确 Mac 的完整密钥，以及 UDP 42791 是否被防火墙阻止。")


def main():
    parser = argparse.ArgumentParser(description="SoundFlow：把 Linux 系统声音自动汇集到 Mac。")
    parser.add_argument("--version", action="version", version=__version__)
    parser.add_argument("--config", type=Path, help="配置文件位置")
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("pair", "discover", "run"):
        sub = commands.add_parser(command)
        sub.add_argument("--host", help="指定 Mac 地址（自动发现受限时使用）")
        sub.add_argument("--discovery-port", type=int, default=42790, help=argparse.SUPPRESS)
        if command == "pair":
            sub.add_argument("--name", help="在 Mac 上显示的设备名称")
        if command == "run":
            sub.add_argument("--monitor-only", action="store_true", help="保留 Linux 本地播放，同时发送声音")
            sub.add_argument("--test-tone", action="store_true", help="发送测试音，不采集系统声音")
            sub.add_argument("--duration", type=float, help=argparse.SUPPRESS)
    commands.add_parser("restore", help="恢复 Linux 本地声音路由")
    commands.add_parser("doctor", help="检查音频服务和配置")
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    config_path, state_path = paths()
    if args.config:
        config_path = args.config
    try:
        if args.command == "discover":
            items = discover(host=args.host, port=args.discovery_port)
            for item in items:
                print(f"{item.name}\t{item.host}:{item.port}\t{item.id}")
            if not items:
                print("未发现 SoundFlow Mac。")
            return 0 if items else 1
        if args.command == "pair":
            pair(args, config_path, state_path)
            return 0
        if args.command == "doctor":
            AudioRoute.check_dependencies()
            print("音频服务：正常（PipeWire-Pulse / PulseAudio）")
            print("配置：" + ("已配对" if config_path.exists() else "尚未配对，请先运行 soundflow pair"))
            print("平台：" + os.uname().machine)
            return 0
        # Serialize run/restore so a second process cannot tear down an active route.
        state_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        lock = open(state_path.parent / "sender.lock", "a")
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("SoundFlow 已在运行。请先用 systemctl --user stop soundflow 停止服务。")
        if args.command == "restore":
            MacMicrophone(state_path.with_name("microphone-route.json")).restore()
            return 0 if AudioRoute(state_path).restore() else 1
        if not config_path.exists():
            raise RuntimeError("尚未配对，请运行 soundflow pair。")
        config = json.loads(config_path.read_text())
        parse_code(config["key"])
        sender = Sender(config, state_path, host=args.host, discovery_port=args.discovery_port,
                        monitor_only=args.monitor_only, test_tone=args.test_tone, duration=args.duration)
        signal.signal(signal.SIGTERM, sender.stop)
        signal.signal(signal.SIGINT, sender.stop)
        sender.run()
        return 0
    except (ValueError, KeyError, OSError, RuntimeError, subprocess.SubprocessError) as exc:
        logging.error("%s", exc)
        return 1


if __name__ == "__main__":
    sys.exit(main())
