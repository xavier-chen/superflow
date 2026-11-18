import json
import os
import struct
import socket
import tempfile
import unittest
import uuid
from pathlib import Path
from unittest.mock import patch

from cryptography.hazmat.primitives.ciphers.aead import ChaCha20Poly1305
from soundflow.audio import AudioRoute, private_json
from soundflow.discovery import _lan_broadcasts
from soundflow.protocol import ACK, AUDIO, HEADER, Cipher, connection_code, parse_code, session_key, MAC_MICROPHONE, LINUX_MICROPHONE
from soundflow.microphone import physical_microphones


class ProtocolTests(unittest.TestCase):
    def test_microphone_replay_and_server_restart(self):
        key, identity = bytes(range(32)), uuid.uuid4()
        cipher = Cipher(key, identity, bytes(range(8)))
        server_session = b"server01"
        cipher.set_mac_session(server_session)
        header = HEADER.pack(b"SF01", MAC_MICROPHONE, identity.bytes, server_session, 0)
        packet = header + ChaCha20Poly1305(session_key(key, identity, server_session, info=b"SoundFlow-v1-mac-microphone")).encrypt(bytes(12), bytes(968), header)
        self.assertEqual(cipher.open_mac_microphone(packet).payload, bytes(968))
        with self.assertRaises(ValueError): cipher.open_mac_microphone(packet)
        cipher.set_mac_session(b"server02")
        with self.assertRaises(ValueError): cipher.open_mac_microphone(packet)
        microphone = cipher.seal(LINUX_MICROPHONE, bytes(968))[0]
        system = Cipher(key, identity, bytes(range(8))).seal(AUDIO, bytes(968))[0]
        self.assertNotEqual(microphone[HEADER.size:], system[HEADER.size:])

    def test_microphone_capture_excludes_output_monitors_and_network_input(self):
        sources = [{"name": "alsa_input.usb", "description": "USB mic"},
                   {"name": "alsa_output.usb.monitor"}, {"name": "soundflow_microphone"}]
        with patch("soundflow.microphone.pactl", return_value=json.dumps(sources)):
            self.assertEqual(physical_microphones(), [{"name": "alsa_input.usb", "description": "USB mic"}])

    def test_unplugged_jacks_do_not_appear_as_available_microphones(self):
        sources = [{"name": "alsa_input.jacks", "ports": [{"availability": "not available"}, {"availability": "not available"}]},
                   {"name": "alsa_input.usb", "description": "USB mic", "ports": [{"availability": "unknown"}]}]
        with patch("soundflow.microphone.pactl", return_value=json.dumps(sources)):
            self.assertEqual(physical_microphones(), [{"name": "alsa_input.usb", "description": "USB mic"}])

    def test_discovery_includes_lan_when_vpn_is_default(self):
        def ioctl(_fd, operation, request):
            name = request.split(b"\0", 1)[0].decode()
            result = bytearray(256)
            if operation == 0x8913:
                struct.pack_into("H", result, 16, {"lo": 0x9, "lan": 0x3, "vpn": 0x1, "down": 0x2}[name])
            else:
                result[20:24] = socket.inet_aton("192.168.10.255")
            return bytes(result)
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            with patch("soundflow.discovery.sys.platform", "linux"), \
                 patch("soundflow.discovery.socket.if_nameindex", return_value=[(1, "lo"), (2, "lan"), (3, "vpn"), (4, "down")]), \
                 patch("fcntl.ioctl", side_effect=ioctl):
                self.assertEqual(_lan_broadcasts(sock), ["192.168.10.255"])

    def test_key_codes(self):
        key = bytes(range(32))
        self.assertEqual(parse_code(connection_code(key)), key)
        for bad in ("", "SF1-", "SF1-%%%%", "SF1-a", "test"):
            with self.assertRaises(ValueError):
                parse_code(bad)

    def test_ack_uses_separate_key_and_validates_identity(self):
        key, identity = bytes(range(32)), uuid.UUID("00112233-4455-6677-8899-aabbccddeeff")
        cipher = Cipher(key, identity, bytes(range(8)))
        header = HEADER.pack(b"SF01", ACK, identity.bytes, cipher.session, 4)
        nonce = b"\0" * 4 + struct.pack("!Q", 4)
        body = ChaCha20Poly1305(session_key(key, identity, cipher.session, reply=True)).encrypt(nonce, b"ack", header)
        self.assertEqual(cipher.open_ack(header + body).payload, b"ack")
        with self.assertRaises(ValueError):
            cipher.open_ack(cipher.seal(AUDIO, b"pcm")[0])
        self.assertNotEqual(session_key(key, identity, cipher.session), session_key(key, identity, cipher.session, reply=True))

    def test_private_config_permissions(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "config.json"
            private_json(path, {"key": "test"})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(json.loads(path.read_text()), {"key": "test"})


class RouteTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.path = Path(self.temp.name) / "route.json"
        private_json(self.path, {"original": "speakers", "module": "30", "sink": "soundflow_output"})

    def tearDown(self):
        self.temp.cleanup()

    def test_restore_respects_user_changed_output_and_only_owns_its_module(self):
        calls = []

        def fake(*args, **kwargs):
            calls.append(args)
            table = {
                ("list", "short", "sinks"): "1 speakers driver\n2 soundflow_output driver\n3 headset driver",
                ("get-default-sink",): "headset",
                ("list", "short", "sink-inputs"): "51 2 client\n52 3 client",
                ("list", "short", "modules"): "30 module-null-sink sink_name=other\n31 module-null-sink sink_name=soundflow_output rate=48000",
            }
            return table.get(args, "")

        with patch("soundflow.audio.pactl", side_effect=fake):
            self.assertTrue(AudioRoute(self.path).restore())
        self.assertIn(("move-sink-input", "51", "headset"), calls)
        self.assertIn(("unload-module", "31"), calls)
        self.assertNotIn(("unload-module", "30"), calls)
        self.assertNotIn(("set-default-sink", "speakers"), calls)
        self.assertFalse(self.path.exists())

    def test_failed_restore_keeps_recovery_state(self):
        with patch("soundflow.audio.pactl", side_effect=RuntimeError("server unavailable")):
            self.assertFalse(AudioRoute(self.path).restore())
        self.assertTrue(self.path.exists())

    def test_missing_sink_clears_stale_state_without_changes(self):
        with patch("soundflow.audio.pactl", return_value="1 speakers driver") as command:
            self.assertTrue(AudioRoute(self.path).restore())
        self.assertEqual(command.call_count, 1)
        self.assertFalse(self.path.exists())

    def test_headless_dummy_sink_is_restored_by_unloading_our_sink(self):
        def fake(*args, **kwargs):
            return {
                ("list", "short", "sinks"): "2 soundflow_output driver",
                ("get-default-sink",): "soundflow_output",
                ("list", "short", "modules"): "31 module-null-sink sink_name=soundflow_output rate=48000",
            }.get(args, "")
        with patch("soundflow.audio.pactl", side_effect=fake) as command:
            self.assertTrue(AudioRoute(self.path).restore())
            command.assert_any_call("unload-module", "31")
        self.assertFalse(self.path.exists())


if __name__ == "__main__":
    unittest.main()
