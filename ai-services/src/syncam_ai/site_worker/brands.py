"""RTSP address templates and fingerprints for common DVR/NVR/camera brands.

Most installed CCTV comes from a few OEM families that share firmware and
therefore RTSP paths. Lorex 4K DVRs, Amcrest and CP Plus are Dahua-based;
HiLook and Annke are Hikvision-based. Channels count from 1.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable, Iterable
from urllib.parse import quote


@dataclass(frozen=True)
class BrandFamily:
    name: str
    label: str
    sub_path: Callable[[int], str]
    main_path: Callable[[int], str]
    keywords: tuple[str, ...]
    signature_ports: tuple[int, ...] = ()


FAMILIES: dict[str, BrandFamily] = {
    "dahua": BrandFamily(
        "dahua", "Dahua / Lorex / Amcrest / CP Plus",
        lambda n: f"/cam/realmonitor?channel={n}&subtype=1",
        lambda n: f"/cam/realmonitor?channel={n}&subtype=0",
        ("dahua", "lorex", "amcrest", "cp plus", "cpplus", "imou", "realmonitor"),
        (37777,),
    ),
    "hikvision": BrandFamily(
        "hikvision", "Hikvision / HiLook / Annke",
        lambda n: f"/Streaming/Channels/{n}02",
        lambda n: f"/Streaming/Channels/{n}01",
        ("hikvision", "hilook", "annke", "dvrdvs", "app-webs", "hik"),
        (8000,),
    ),
    "uniview": BrandFamily(
        "uniview", "Uniview",
        lambda n: f"/unicast/c{n}/s1/live",
        lambda n: f"/unicast/c{n}/s0/live",
        ("uniview", "unv"),
    ),
    "reolink": BrandFamily(
        "reolink", "Reolink",
        lambda n: f"/h264Preview_{n:02d}_sub",
        lambda n: f"/h264Preview_{n:02d}_main",
        ("reolink",),
        (9000,),
    ),
}


def guess_family(texts: Iterable[str], open_ports: Iterable[int]) -> str | None:
    """Best guess from ONVIF scopes, RTSP/HTTP server banners and open ports."""

    haystack = " ".join(text.lower() for text in texts if text)
    for family in FAMILIES.values():
        if any(keyword in haystack for keyword in family.keywords):
            return family.name
    ports = set(open_ports)
    for family in FAMILIES.values():
        if ports.intersection(family.signature_ports):
            return family.name
    return None


def family_order(preferred: str | None) -> list[str]:
    """Families to try, the likely one first."""

    names = list(FAMILIES)
    if preferred in FAMILIES:
        names.remove(preferred)
        names.insert(0, preferred)
    return names


def rtsp_path(family: str, channel: int, *, stream: str = "sub") -> str:
    brand = FAMILIES[family]
    return (brand.main_path if stream == "main" else brand.sub_path)(channel)


def rtsp_url(host: str, port: int, family: str, channel: int, username: str = "", password: str = "", *, stream: str = "sub") -> str:
    """A full RTSP address; credentials are percent-encoded so any password works."""

    auth = f"{quote(username, safe='')}:{quote(password, safe='')}@" if username else ""
    return f"rtsp://{auth}{host}:{port}{rtsp_path(family, channel, stream=stream)}"
