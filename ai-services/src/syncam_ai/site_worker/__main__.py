"""Command line for the SyncCam site box.

    python -m syncam_ai.site_worker check    --config site.json
    python -m syncam_ai.site_worker snapshot --config site.json --camera <id> --out frame.jpg
    python -m syncam_ai.site_worker run      --config site.json
"""

from __future__ import annotations

import argparse
import logging
import signal
import sys
import time
from pathlib import Path

from .capture import CameraStream, redact
from .client import BackendClient, BackendError
from .config import LocalConfig, load_local_config
from .runner import FrameProcessor, SiteWorker
from .spool import Spool

_LOG = logging.getLogger("syncam.site_worker")


def _detector(config: LocalConfig):  # type: ignore[no-untyped-def]
    from .detector import YoloxDetector

    return YoloxDetector(config.detector_model, device=config.device)


def _processors(config: LocalConfig, client: BackendClient) -> list[FrameProcessor]:
    processors: list[FrameProcessor] = []
    if config.weapon_model is not None:
        from .weapons import WeaponProcessor

        processors.append(WeaponProcessor.from_config(config))
    if config.face_models_dir is not None and config.entrance_cameras:
        from .faces import AttendanceProcessor

        processors.append(AttendanceProcessor.from_config(config, client))
    return processors


def _start_streams(config: LocalConfig) -> dict[str, CameraStream]:
    streams = {camera_id: CameraStream(camera_id, source) for camera_id, source in config.cameras.items()}
    for stream in streams.values():
        stream.start()
    return streams


def command_check(config: LocalConfig) -> int:
    ok = True
    client = BackendClient(config.backend_url, config.site_key)
    try:
        site = client.config()
        names = {camera["id"]: camera.get("name", "") for camera in site.get("cameras", [])}
        print(f"Backend: connected to site '{site.get('site', {}).get('name', '')}' with {len(names)} camera(s) and {len(site.get('zones', []))} zone(s).")
        for camera_id in config.cameras:
            label = names.get(camera_id)
            print(f"  camera {camera_id}: {'registered as ' + repr(label) if label is not None else 'NOT registered in the dashboard'}")
            ok = ok and label is not None
    except BackendError as error:
        print(f"Backend: FAILED - {error}")
        ok = False
    try:
        detector = _detector(config)
        print(f"Detector: {detector.model_version} loaded on {config.device}.")
    except Exception as error:
        print(f"Detector: FAILED - {error}")
        return 1
    streams = _start_streams(config)
    deadline = time.monotonic() + 20
    pending = set(streams)
    while pending and time.monotonic() < deadline:
        for camera_id in list(pending):
            latest = streams[camera_id].latest()
            if latest is not None:
                pending.discard(camera_id)
                started = time.perf_counter()
                found = detector.detect(latest[1])
                elapsed = (time.perf_counter() - started) * 1000
                counts: dict[str, int] = {}
                for item in found:
                    counts[item.subject_class] = counts.get(item.subject_class, 0) + 1
                print(f"Camera {camera_id}: frame {latest[1].shape[1]}x{latest[1].shape[0]}, detected {counts or 'nothing'} in {elapsed:.0f} ms")
        time.sleep(0.2)
    for camera_id in pending:
        print(f"Camera {camera_id}: NO FRAMES from {redact(config.cameras[camera_id])} within 20 s")
        ok = False
    for stream in streams.values():
        stream.stop()
    return 0 if ok else 1


def command_snapshot(config: LocalConfig, camera_id: str, output: Path) -> int:
    import cv2

    if camera_id not in config.cameras:
        print("camera is not in the local config")
        return 1
    stream = CameraStream(camera_id, config.cameras[camera_id])
    stream.start()
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        latest = stream.latest()
        if latest is not None:
            cv2.imwrite(str(output), latest[1])
            print(f"saved {output}")
            stream.stop()
            return 0
        time.sleep(0.2)
    print("no frame received within 20 s")
    stream.stop()
    return 1


def command_enroll(config: LocalConfig, staff_id: str, photos: Path) -> int:
    """Enroll a consenting staff member from a folder of their photos."""

    import cv2

    from .faces import FaceEngine, FaceGallery

    if config.face_models_dir is None:
        print("Set models.faces in the config to the face model folder first.")
        return 1
    staff = {item["id"]: item.get("display_name", "") for item in BackendClient(config.backend_url, config.site_key).config().get("staff", [])}
    if staff_id not in staff:
        print("This staff ID is not an active staff member of this site. Add them in the dashboard (Attendance) with recorded consent first.")
        return 1
    engine = FaceEngine(config.face_models_dir, device=config.device)
    signatures = []
    for path in sorted(photos.iterdir()):
        if path.suffix.lower() not in {".jpg", ".jpeg", ".png"}:
            continue
        image = cv2.imread(str(path))
        signature = engine.largest_face_signature(image) if image is not None else None
        print(f"  {path.name}: {'face found' if signature else 'NO FACE - skipped'}")
        if signature:
            signatures.append(signature)
    if len(signatures) < 3:
        print("At least 3 clear photos of the person's face are needed (different angles and lighting help).")
        return 1
    FaceGallery.from_environment(config.face_gallery).enroll(staff_id, staff[staff_id], signatures)
    print(f"Enrolled {staff[staff_id]} with {len(signatures)} photos. The photos themselves were not stored; you can delete them now.")
    return 0


def command_forget(config: LocalConfig, staff_id: str) -> int:
    from .faces import FaceGallery

    removed = FaceGallery.from_environment(config.face_gallery).forget(staff_id)
    print("Face data deleted from this site box." if removed else "No face data was stored for that staff ID.")
    return 0


def command_run(config: LocalConfig) -> int:
    client = BackendClient(config.backend_url, config.site_key)
    detector = _detector(config)
    streams = _start_streams(config)
    worker = SiteWorker(
        backend=client,
        detector=detector,
        streams=streams,
        spool=Spool(config.spool_dir),
        fps=config.fps,
        min_confidence=config.min_confidence,
        processors=_processors(config, client),
    )

    def _stop(*_: object) -> None:
        _LOG.info("stopping site worker")
        worker.stop()
        for stream in streams.values():
            stream.stop()

    signal.signal(signal.SIGINT, _stop)
    signal.signal(signal.SIGTERM, _stop)
    _LOG.info("site worker running with %d camera(s) at %.1f fps", len(streams), config.fps)
    worker.run()
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m syncam_ai.site_worker", description="SyncCam AI site box")
    parser.add_argument("command", choices=["run", "check", "snapshot", "enroll", "forget", "face-key"])
    parser.add_argument("--config", type=Path, help="local site JSON file (outside Git)")
    parser.add_argument("--camera", help="camera ID for the snapshot command")
    parser.add_argument("--out", type=Path, default=Path("snapshot.jpg"))
    parser.add_argument("--staff-id", help="staff ID from the dashboard for enroll/forget")
    parser.add_argument("--photos", type=Path, help="folder of the staff member's face photos for enroll")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args(argv)
    logging.basicConfig(level=logging.DEBUG if args.verbose else logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    if args.command == "face-key":
        from .faces import FaceGallery

        print("Store this key as the SYNCAM_FACE_KEY environment variable on this site box only.")
        print("Losing it means re-enrolling staff; never copy it to the cloud or into Git.")
        print(FaceGallery.new_key())
        return 0
    if args.config is None:
        parser.error("--config is required")
    try:
        config = load_local_config(args.config)
    except (OSError, ValueError) as error:
        print(f"config error: {error}", file=sys.stderr)
        return 2
    if args.command == "check":
        return command_check(config)
    if args.command == "snapshot":
        if not args.camera:
            parser.error("--camera is required for snapshot")
        return command_snapshot(config, args.camera, args.out)
    if args.command in {"enroll", "forget"}:
        if not args.staff_id:
            parser.error("--staff-id is required")
        if args.command == "forget":
            return command_forget(config, args.staff_id)
        if args.photos is None or not args.photos.is_dir():
            parser.error("--photos must be a folder of face photos")
        return command_enroll(config, args.staff_id, args.photos)
    return command_run(config)


if __name__ == "__main__":
    raise SystemExit(main())
