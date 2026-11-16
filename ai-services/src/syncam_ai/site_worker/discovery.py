"""Find DVRs, NVRs and IP cameras on the site's network (standard library only).

1. ONVIF WS-Discovery: a multicast probe that most cameras and recorders
   answer with their address and a description (make, model).
2. A quick TCP scan of the box's own /24 subnet for RTSP (554) and the
   brand-specific service ports, for devices with ONVIF switched off.
3. An unauthenticated RTSP OPTIONS reads the server banner for the brand.

With the device password, :func:`probe_channels` asks the recorder for each
channel with RTSP DESCRIBE (Digest or Basic auth) and grabs one small
thumbnail per working channel. On a wrong password it stops at once, because
many recorders lock the account after a few failures.
"""

from __future__ import annotations

import base64
import hashlib
import ipaddress
import logging
import re
import socket
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Any, Callable, Iterable

from .brands import FAMILIES, family_order, guess_family, rtsp_path, rtsp_url

_LOG = logging.getLogger(__name__)
SCAN_PORTS = (554, 37777, 8000, 9000, 80)
WS_DISCOVERY = ("239.255.255.250", 3702)
_PROBE = (
    '<?xml version="1.0" encoding="UTF-8"?>'
    '<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" '
    'xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" '
    'xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" '
    'xmlns:dn="http://www.onvif.org/ver10/network/wsdl">'
    "<e:Header><w:MessageID>uuid:{message_id}</w:MessageID>"
    "<w:To e:mustUnderstand=\"true\">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>"
    "<w:Action e:mustUnderstand=\"true\">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>"
    "</e:Header><e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>"
)


@dataclass
class FoundDevice:
    ip: str
    rtsp_port: int = 554
    ports: set[int] = field(default_factory=set)
    family: str | None = None
    name: str = ""
    model: str = ""
    server: str = ""
    onvif: bool = False

    @property
    def device_id(self) -> str:
        return f"{self.ip}:{self.rtsp_port}"

    def as_dict(self) -> dict[str, Any]:
        return {
            "device_id": self.device_id, "ip": self.ip, "rtsp_port": self.rtsp_port, "ports": sorted(self.ports),
            "family": self.family, "brand": FAMILIES[self.family].label if self.family else "Unknown brand",
            "name": self.name, "model": self.model, "onvif": self.onvif,
        }


# ---------------------------------------------------------------- network

def local_networks() -> list[ipaddress.IPv4Network]:
    """The private /24 networks this computer is on."""

    addresses: set[str] = set()
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
            probe.connect(("10.255.255.255", 1))  # no packet is sent for UDP connect
            addresses.add(probe.getsockname()[0])
    except OSError:
        pass
    try:
        addresses.update(socket.gethostbyname_ex(socket.gethostname())[2])
    except OSError:
        pass
    networks = []
    for address in sorted(addresses):
        ip = ipaddress.ip_address(address)
        if ip.version == 4 and ip.is_private and not ip.is_loopback and not ip.is_link_local:
            network = ipaddress.ip_network(f"{address}/24", strict=False)
            if network not in networks:
                networks.append(network)
    return networks


def parse_probe_match(payload: str) -> tuple[list[str], str]:
    """(XAddrs, Scopes) from a WS-Discovery ProbeMatch message."""

    xaddrs = re.search(r"<[^>]*XAddrs[^>]*>([^<]*)<", payload)
    scopes = re.search(r"<[^>]*Scopes[^>]*>([^<]*)<", payload)
    return (xaddrs.group(1).split() if xaddrs else []), (scopes.group(1) if scopes else "")


def onvif_scope_value(scopes: str, key: str) -> str:
    match = re.search(rf"onvif://www\.onvif\.org/{key}/(\S+)", scopes)
    return match.group(1).replace("%20", " ").replace("_", " ") if match else ""


def ws_discovery(timeout: float = 3.0) -> dict[str, FoundDevice]:
    """Devices that answer an ONVIF WS-Discovery probe, keyed by IP."""

    found: dict[str, FoundDevice] = {}
    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM, socket.IPPROTO_UDP)
    except OSError:
        return found
    with sock:
        sock.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_TTL, 2)
        sock.settimeout(0.5)
        message = _PROBE.format(message_id=uuid.uuid4()).encode()
        try:
            sock.sendto(message, WS_DISCOVERY)
        except OSError as error:
            _LOG.debug("WS-Discovery probe failed: %s", error)
            return found
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                payload, (sender, _) = sock.recvfrom(65535)
            except socket.timeout:
                continue
            except OSError:
                break
            xaddrs, scopes = parse_probe_match(payload.decode("utf-8", "replace"))
            device = found.setdefault(sender, FoundDevice(ip=sender))
            device.onvif = True
            device.name = onvif_scope_value(scopes, "name") or device.name
            device.model = onvif_scope_value(scopes, "hardware") or device.model
            device.server = " ".join(filter(None, [device.server, scopes, " ".join(xaddrs)]))
    return found


def open_ports(hosts: Iterable[str], ports: Iterable[int] = SCAN_PORTS, *, timeout: float = 0.4, workers: int = 128) -> dict[str, set[int]]:
    """Which of ``ports`` accept TCP connections on each host."""

    targets = [(host, port) for host in hosts for port in ports]

    def check(target: tuple[str, int]) -> tuple[str, int] | None:
        try:
            with socket.create_connection(target, timeout=timeout):
                return target
        except OSError:
            return None

    result: dict[str, set[int]] = {}
    with ThreadPoolExecutor(max_workers=workers) as pool:
        for hit in pool.map(check, targets):
            if hit:
                result.setdefault(hit[0], set()).add(hit[1])
    return result


# ---------------------------------------------------------------- RTSP

@dataclass(frozen=True)
class RtspReply:
    status: int
    headers: dict[str, str]


def _rtsp_exchange(host: str, port: int, request: str, timeout: float) -> RtspReply:
    with socket.create_connection((host, port), timeout=timeout) as sock:
        sock.settimeout(timeout)
        sock.sendall(request.encode())
        data = b""
        while b"\r\n\r\n" not in data and len(data) < 65536:
            chunk = sock.recv(4096)
            if not chunk:
                break
            data += chunk
    head = data.split(b"\r\n\r\n", 1)[0].decode("latin-1")
    lines = head.split("\r\n")
    match = re.match(r"RTSP/1\.\d\s+(\d{3})", lines[0] if lines else "")
    if not match:
        raise ConnectionError("not an RTSP server")
    headers: dict[str, str] = {}
    for line in lines[1:]:
        if ":" in line:
            name, value = line.split(":", 1)
            headers[name.strip().lower()] = value.strip()
    return RtspReply(int(match.group(1)), headers)


def digest_authorization(challenge: str, method: str, uri: str, username: str, password: str) -> str:
    """An RFC 2617 Digest Authorization header (MD5, no qop or qop=auth)."""

    fields = dict(re.findall(r'(\w+)="?([^",]*)"?', challenge))
    realm, nonce = fields.get("realm", ""), fields.get("nonce", "")
    ha1 = hashlib.md5(f"{username}:{realm}:{password}".encode()).hexdigest()
    ha2 = hashlib.md5(f"{method}:{uri}".encode()).hexdigest()
    parts = [f'username="{username}"', f'realm="{realm}"', f'nonce="{nonce}"', f'uri="{uri}"']
    if "auth" in fields.get("qop", "").split(","):
        cnonce, nc = uuid.uuid4().hex[:16], "00000001"
        response = hashlib.md5(f"{ha1}:{nonce}:{nc}:{cnonce}:auth:{ha2}".encode()).hexdigest()
        parts += ["qop=auth", f"nc={nc}", f'cnonce="{cnonce}"']
    else:
        response = hashlib.md5(f"{ha1}:{nonce}:{ha2}".encode()).hexdigest()
    parts.append(f'response="{response}"')
    if fields.get("opaque"):
        parts.append(f'opaque="{fields["opaque"]}"')
    return "Digest " + ", ".join(parts)


def rtsp_request(host: str, port: int, method: str, path: str = "", username: str = "", password: str = "", *, timeout: float = 4.0) -> RtspReply:
    """One RTSP request, answering a single Digest/Basic challenge if needed."""

    uri = f"rtsp://{host}:{port}{path}" if path else f"rtsp://{host}:{port}"

    def request(cseq: int, authorization: str = "") -> str:
        lines = [f"{method} {uri} RTSP/1.0", f"CSeq: {cseq}", "User-Agent: SyncCam-site-box"]
        if method == "DESCRIBE":
            lines.append("Accept: application/sdp")
        if authorization:
            lines.append(f"Authorization: {authorization}")
        return "\r\n".join(lines) + "\r\n\r\n"

    reply = _rtsp_exchange(host, port, request(1), timeout)
    challenge = reply.headers.get("www-authenticate", "")
    if reply.status != 401 or not username or not challenge:
        return reply
    if challenge.lower().startswith("digest"):
        authorization = digest_authorization(challenge, method, uri, username, password)
    else:
        authorization = "Basic " + base64.b64encode(f"{username}:{password}".encode()).decode()
    return _rtsp_exchange(host, port, request(2, authorization), timeout)


# ---------------------------------------------------------------- discovery

def discover(*, timeout: float = 3.0, networks: list[ipaddress.IPv4Network] | None = None) -> list[FoundDevice]:
    """Every likely camera, DVR or NVR on the local network."""

    devices = ws_discovery(timeout)
    hosts = [str(host) for network in (networks if networks is not None else local_networks()) for host in network.hosts()]
    for host, ports in open_ports(hosts).items():
        device = devices.setdefault(host, FoundDevice(ip=host))
        device.ports |= ports
    result = []
    for device in devices.values():
        if 554 not in device.ports and not device.onvif:
            continue  # web servers, printers and the like
        try:
            reply = rtsp_request(device.ip, device.rtsp_port, "OPTIONS", timeout=2.0)
            device.server = " ".join(filter(None, [device.server, reply.headers.get("server", "")]))
        except (OSError, ConnectionError):
            if not device.onvif:
                continue
        device.family = guess_family([device.server, device.name, device.model], device.ports)
        result.append(device)
    return sorted(result, key=lambda item: ipaddress.ip_address(item.ip))


@dataclass
class ProbeResult:
    family: str | None = None
    channels: list[dict[str, Any]] = field(default_factory=list)
    error: str = ""

    def as_dict(self) -> dict[str, Any]:
        return {"family": self.family, "brand": FAMILIES[self.family].label if self.family else "", "channels": self.channels, "error": self.error}


def probe_channels(
    host: str,
    port: int,
    username: str,
    password: str,
    *,
    family: str | None = None,
    max_channels: int = 32,
    misses_to_stop: int = 4,
    thumbnail: Callable[[str], dict[str, Any] | None] | None = None,
    request: Callable[..., RtspReply] = rtsp_request,
) -> ProbeResult:
    """Find the brand template that works, then every channel that answers."""

    def describe(name: str, channel: int) -> int:
        try:
            return request(host, port, "DESCRIBE", rtsp_path(name, channel), username, password).status
        except (OSError, ConnectionError):
            return 0

    chosen = None
    for name in family_order(family):
        status = describe(name, 1)
        if status == 401:
            return ProbeResult(family=family, error="The username or password was not accepted. Check them on the recorder (too many wrong tries can lock the account).")
        if status == 200:
            chosen = name
            break
        if status == 0 and name == family_order(family)[0]:
            return ProbeResult(family=family, error=f"Could not reach RTSP on {host}:{port}. Check that RTSP is enabled on the recorder.")
    if chosen is None:
        return ProbeResult(family=family, error="The recorder answered but no known RTSP address worked. It may need RTSP enabled, or it may use a format we do not know yet.")
    result = ProbeResult(family=chosen)
    misses = 0
    for channel in range(1, max_channels + 1):
        status = 200 if channel == 1 else describe(chosen, channel)
        if status == 401:
            result.error = "The recorder stopped accepting the password while checking channels."
            break
        if status != 200:
            misses += 1
            if misses >= misses_to_stop:
                break
            continue
        misses = 0
        entry: dict[str, Any] = {"channel": channel}
        if thumbnail is not None:
            shot = thumbnail(rtsp_url(host, port, chosen, channel, username, password))
            if shot:
                entry.update(shot)
        result.channels.append(entry)
    return result


def grab_thumbnail(url: str, *, width: int = 320, timeout_ms: int = 6000) -> dict[str, Any] | None:
    """One small JPEG (base64) from an RTSP address, or None."""

    import cv2

    capture = cv2.VideoCapture(url, cv2.CAP_FFMPEG, [cv2.CAP_PROP_OPEN_TIMEOUT_MSEC, timeout_ms, cv2.CAP_PROP_READ_TIMEOUT_MSEC, timeout_ms])
    try:
        if not capture.isOpened():
            return None
        frame = None
        for _ in range(5):  # skip a couple of frames so the first keyframe decodes
            ok, candidate = capture.read()
            if ok and candidate is not None:
                frame = candidate
                break
        if frame is None:
            return None
        height, original_width = frame.shape[:2]
        small = cv2.resize(frame, (width, max(1, int(height * width / original_width))), interpolation=cv2.INTER_AREA)
        ok, encoded = cv2.imencode(".jpg", small, [cv2.IMWRITE_JPEG_QUALITY, 60])
        if not ok:
            return None
        return {"thumbnail": base64.b64encode(encoded.tobytes()).decode(), "width": original_width, "height": height}
    finally:
        capture.release()
