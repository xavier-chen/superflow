import ipaddress
import json
import secrets
import select
import socket
import struct
import sys
import time
from dataclasses import dataclass

from .protocol import DISCOVERY_PORT, MULTICAST


@dataclass(frozen=True)
class Receiver:
    id: str
    name: str
    host: str
    port: int
    challenge: str


def _lan_broadcasts(sock):
    """Probe physical subnets too when a VPN owns the default multicast route."""
    if sys.platform != "linux":
        return []
    import fcntl
    broadcasts = set()
    for _, name in socket.if_nameindex():
        request = struct.pack("256s", name.encode()[:15])
        try:
            flags = struct.unpack_from("H", fcntl.ioctl(sock.fileno(), 0x8913, request), 16)[0]
            if flags & 0x3 != 0x3 or flags & 0x8:  # UP + BROADCAST, excluding loopback
                continue
            address = socket.inet_ntoa(fcntl.ioctl(sock.fileno(), 0x8919, request)[20:24])
            if address not in ("0.0.0.0", "255.255.255.255"):
                broadcasts.add(address)
        except OSError:
            continue
    return sorted(broadcasts)


def discover(timeout=2.0, host=None, port=DISCOVERY_PORT):
    """Multicast + broadcast; replies are unicast to the probe's ephemeral port."""
    request = secrets.token_hex(12)
    query = json.dumps({"type": "soundflow-discover-v1", "request": request}).encode()
    found = {}
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
        sock.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_TTL, 1)
        sock.bind(("0.0.0.0", 0))
        sock.setblocking(False)
        targets = [socket.gethostbyname(host)] if host else [MULTICAST, "255.255.255.255", *_lan_broadcasts(sock)]
        deadline, next_send = time.monotonic() + timeout, 0.0
        while time.monotonic() < deadline:
            now = time.monotonic()
            if now >= next_send:
                for target in targets:
                    try:
                        sock.sendto(query, (target, port))
                    except OSError:
                        pass
                next_send = now + 0.6
            ready, _, _ = select.select([sock], [], [], max(0, min(0.2, deadline - now)))
            if not ready:
                continue
            try:
                data, source = sock.recvfrom(2048)
                offer = json.loads(data)
                if offer.get("type") != "soundflow-offer-v1" or offer.get("request") != request:
                    continue
                import uuid
                identity = str(uuid.UUID(offer["id"]))
                audio_port = int(offer["port"])
                if not 1 <= audio_port <= 65535:
                    continue
                challenge = offer.get("challenge", "")
                if not isinstance(challenge, str) or not 16 <= len(challenge) <= 128:
                    continue
                # Never trust an address supplied inside the discovery payload.
                ipaddress.IPv4Address(source[0])
                found[identity] = Receiver(identity, str(offer.get("name", "Mac"))[:80], source[0], audio_port, challenge)
            except (ValueError, KeyError, TypeError, OSError):
                continue
    return sorted(found.values(), key=lambda item: (item.name, item.id))
