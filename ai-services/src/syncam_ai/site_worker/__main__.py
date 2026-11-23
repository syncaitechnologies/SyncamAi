"""Command line for the SyncCam site box.

    python -m syncam_ai.site_worker check    --config site.json
    python -m syncam_ai.site_worker snapshot --config site.json --camera <id> --out frame.jpg
    python -m syncam_ai.site_worker run      --config site.json
    python -m syncam_ai.site_worker liveness --config site.json [--camera <id>]
    python -m syncam_ai.site_worker pair     --config site.json --code K7Q4-XM92

The installed program (syncam-box.exe) takes the same commands; without
--config it uses the box folder (%ProgramData%\\SyncCam, see home.py):

    syncam-box.exe setup --code-file <file>   create the folder, download the model, pair
    syncam-box.exe run --log-file <file>      what the start-up task runs
    syncam-box.exe update-check | selftest | --version
"""

from __future__ import annotations

import argparse
import json
import logging
import logging.handlers
import signal
import sys
import threading
import time
from pathlib import Path
from typing import Any

from .capture import CameraStream, redact
from .client import BackendClient, BackendError
from .config import LocalConfig, NotPairedError, load_local_config
from .runner import FrameProcessor, SiteWorker
from .spool import Spool
from .version import VERSION

_LOG = logging.getLogger("syncam.site_worker")


def _installed() -> bool:
    """True inside the PyInstaller build that SyncCamSetup.exe installs."""

    return bool(getattr(sys, "frozen", False))


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


def command_liveness(config: LocalConfig, camera_id: str | None, seconds: float) -> int:
    """Print the liveness score of the largest face once a second (for set-up and demos)."""

    from .faces import FaceEngine, LivenessChecker

    if config.face_models_dir is None or config.liveness_model is None:
        print("Set models.faces and models.liveness in the config first.")
        return 1
    camera_id = camera_id or (config.entrance_cameras[0] if config.entrance_cameras else next(iter(config.cameras)))
    if camera_id not in config.cameras:
        print("camera is not in the local config")
        return 1
    engine = FaceEngine(config.face_models_dir, device=config.device)
    checker = LivenessChecker(config.liveness_model, device=config.device)
    stream = CameraStream(camera_id, config.cameras[camera_id])
    stream.start()
    print(f"Look at camera {camera_id}; then hold a photo or phone screen of a face up to it. Real faces need {config.face_liveness_threshold:.2f} or more.")
    deadline = time.monotonic() + seconds
    try:
        while time.monotonic() < deadline:
            time.sleep(1)
            latest = stream.latest()
            if latest is None:
                continue
            faces = engine.faces(latest[1])
            if not faces:
                print("  no face")
                continue
            largest = max(faces, key=lambda face: (face[2] - face[0]) * (face[3] - face[1]))
            score = checker.score(latest[1], largest[:4])
            verdict = "REAL" if score >= config.face_liveness_threshold else "SPOOF / not accepted"
            print(f"  liveness {score:.2f}  {verdict}")
    finally:
        stream.stop()
    return 0


def command_pair(config_path: Path, code: str, name: str | None) -> int:
    """Pair this box with the code shown in the dashboard (Configuration > Site box)."""

    import json

    from .config import vault_path_for
    from .pairing import pair_box
    from .vault import BoxVault

    try:
        raw = json.loads(config_path.read_text(encoding="utf-8"))
        backend_url = str(raw["backend_url"])
        vault = BoxVault(vault_path_for(raw, config_path))
    except (OSError, ValueError, KeyError) as error:
        print(f"config error: {error}", file=sys.stderr)
        return 2
    if vault.exists():
        print(f"This box is already paired ({vault.path}). Delete that file to pair it again.")
        return 1
    try:
        result = pair_box(vault, backend_url, code, label=name)
    except ValueError as error:
        print(error)
        return 2
    except BackendError as error:
        print(f"Pairing failed: {error}")
        return 1
    print(f"Paired with site {result.site_id}. The site key is stored encrypted in {vault.path}.")
    print("Start the box with: python -m syncam_ai.site_worker run --config <file>")
    return 0


def command_setup(code: str | None, name: str | None, backend_url: str) -> int:
    """What the installer runs: box folder, detection model and pairing."""

    from .home import box_home, ensure_home
    from .model_download import fetch_model
    from .pairing import pair_box
    from .vault import BoxVault

    result = 0
    home = box_home()
    try:
        config_path = ensure_home(home, backend_url)
        raw = json.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        _LOG.error("could not create the box folder %s: %s", home, error)
        return 2
    _LOG.info("box folder: %s", home)
    try:
        detector = Path(str(raw["models"]["detector"]))
        _LOG.info("detection model %s: %s", detector, "downloaded and verified" if fetch_model(detector) else "already present and verified")
    except (OSError, ValueError, KeyError) as error:
        _LOG.error("could not download the detection model (the box retries at start): %s", error)
        result = 1
    vault = BoxVault(Path(str(raw.get("vault") or home / "syncam-box.vault")))
    if vault.exists():
        _LOG.info("this box is already paired")
    elif code:
        try:
            paired = pair_box(vault, str(raw["backend_url"]), code, label=name)
            _LOG.info("paired with site %s", paired.site_id)
        except (ValueError, BackendError) as error:
            _LOG.error("pairing failed: %s. Create a new code in the dashboard and run: syncam-box.exe pair --code <code>", error)
            result = 1
    else:
        _LOG.warning("not paired yet: run syncam-box.exe pair --code <code from the dashboard>")
    return result


def command_fetch_models(config_path: Path) -> int:
    from .model_download import fetch_model

    raw = json.loads(config_path.read_text(encoding="utf-8"))
    detector = Path(str((raw.get("models") or {}).get("detector", "")))
    try:
        fetch_model(detector)
    except (OSError, ValueError) as error:
        print(f"Model download failed: {error}")
        return 1
    print(f"Detection model ready: {detector}")
    return 0


def command_update_check() -> int:
    from .updater import UpdatesDisabled, check_for_update

    try:
        info = check_for_update(VERSION)
    except UpdatesDisabled as error:
        print(error)
        return 1
    except Exception as error:
        print(f"Update check failed: {error}")
        return 1
    print(f"Version {info.version} is available: {info.url}" if info else f"Version {VERSION} is up to date.")
    return 0


def command_selftest() -> int:
    """Check that the bundled libraries load (used by the installer build)."""

    from zoneinfo import ZoneInfo

    import cv2
    import numpy
    import openvino
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

    from .updater import verify_manifest  # noqa: F401 - import check only

    Ed25519PrivateKey.generate()
    ZoneInfo("Asia/Kolkata")
    core = openvino.Core()
    devices = core.available_devices
    print(f"syncam-box {VERSION}: numpy {numpy.__version__}, opencv {cv2.__version__}, openvino {openvino.__version__} ({', '.join(devices)})")
    if "CPU" not in devices:
        print("FAILED: OpenVINO has no CPU device; its plugins are missing from this build.")
        return 1
    # Compile a one-layer model on the CPU: the same path the detector takes.
    from openvino import opset13 as ops

    data = ops.parameter([1, 3, 8, 8], numpy.float32)
    compiled = core.compile_model(openvino.Model([ops.relu(data)], [data]), "CPU")
    output = compiled(numpy.full((1, 3, 8, 8), -1.0, dtype=numpy.float32))[compiled.output(0)]
    if float(output.max()) != 0.0:
        print("FAILED: OpenVINO returned a wrong result on the CPU.")
        return 1
    print("OpenVINO compiled and ran a test model on the CPU.")
    return 0


def _status_server(port: int, provider: Any) -> Any:
    if not port:
        return None
    from .status import StatusServer

    try:
        server = StatusServer(provider, port=port)
    except OSError as error:
        _LOG.warning("status page not started on port %d: %s", port, error)
        return None
    server.start()
    _LOG.info("status page: http://127.0.0.1:%d", server.port)
    return server


def wait_until_paired(config_path: Path, *, poll_seconds: float = 5.0) -> LocalConfig:
    """Keep an unpaired installed box alive, with its status page, until it is paired."""

    raw = json.loads(config_path.read_text(encoding="utf-8"))
    message = {"message": f"Version {VERSION}. Waiting for pairing."}
    server = _status_server(int(raw.get("status_port", 8790)), lambda: {"version": VERSION, "paired": False, "worker": {}, "update": message})
    _LOG.warning("this box is not paired: run syncam-box.exe pair --code <code from the dashboard>")
    try:
        while True:
            time.sleep(poll_seconds)
            try:
                return load_local_config(config_path)
            except NotPairedError:
                continue
    finally:
        if server is not None:
            server.stop()


SETTINGS_HELP = "Run the SyncCam installer again as Administrator. If that does not help, contact SyncCam."


def wait_until_config_readable(
    config_path: Path, error: Exception, *, poll_seconds: float | None = None, port: int | None = None
) -> LocalConfig:
    """Keep an installed box's status page up while its settings cannot be read.

    Exiting instead would leave a blank "can't be reached" page and, before
    0.1.6, an empty log. The settings are read again every 30 seconds, so a
    repair (or a reinstall) takes effect without a restart.
    """

    from .home import STATUS_PORT

    poll = RETRY_SECONDS if poll_seconds is None else poll_seconds
    state = {"problem": f"This box cannot read its settings ({error}). {SETTINGS_HELP}"}
    _LOG.error("the site box cannot read its settings in %s: %s; trying again every %.0f seconds", config_path, error, poll)
    server = _status_server(
        STATUS_PORT if port is None else port,
        lambda: {"version": VERSION, "paired": None, "worker": {}, "update": {"message": f"Version {VERSION}."}, "problem": state["problem"]},
    )
    try:
        while True:
            time.sleep(poll)
            try:
                config = load_local_config(config_path)
            except NotPairedError:
                break
            except (OSError, ValueError) as retry_error:
                state["problem"] = f"This box cannot read its settings ({retry_error}). {SETTINGS_HELP}"
                continue
            _LOG.info("the settings can be read again; starting the site box")
            return config
    finally:
        if server is not None:
            server.stop()
    return wait_until_paired(config_path)


def command_forget(config: LocalConfig, staff_id: str) -> int:
    from .faces import FaceGallery

    removed = FaceGallery.from_environment(config.face_gallery).forget(staff_id)
    print("Face data deleted from this site box." if removed else "No face data was stored for that staff ID.")
    return 0


# After a failed start (no internet at boot, a damaged model download) the
# installed box tries again after this long instead of exiting.
RETRY_SECONDS = 30.0


class _Session:
    """Lets Ctrl+C, a service stop or a dashboard restart end the current run."""

    def __init__(self) -> None:
        self.stopping = threading.Event()
        self.restart = threading.Event()
        self._lock = threading.Lock()
        self._stop: Any = None

    def attach(self, stop: Any) -> None:
        with self._lock:
            self._stop = stop
        if self.stopping.is_set():
            stop()

    def detach(self) -> None:
        with self._lock:
            self._stop = None

    def _call_stop(self) -> None:
        with self._lock:
            stop = self._stop
        if stop is not None:
            stop()

    def request_stop(self, *_: object) -> None:
        self.stopping.set()
        self._call_stop()

    def request_restart(self) -> None:
        self.restart.set()
        self._call_stop()


def _confirm_restart(client: Any, task: Any) -> None:
    """Tell the dashboard the restart was received before stopping."""

    try:
        client.post_setup_result(str(task.get("id", "")), {"status": "done", "result": {"restarting": True}})
    except BackendError as error:
        _LOG.warning("could not confirm the restart to the dashboard: %s", error)


def route_tasks(tasks: list[Any], client: Any, session: _Session, agent: Any) -> None:
    """A restart is handled here; "Find cameras" tasks go to the setup agent."""

    setup = []
    restart = False
    for task in tasks:
        if str(task.get("type")) == "restart":
            _confirm_restart(client, task)
            restart = True
        else:
            setup.append(task)
    if setup and agent is not None:
        agent.handle(setup)
    if restart:
        session.request_restart()


def command_run(config: LocalConfig, config_path: Path | None = None) -> int:
    """Run the box until it is stopped.

    A restart from the dashboard stops the cameras and AI and starts them
    again in this process, with the config read again from disk. On the
    installed box a failed start is retried every 30 seconds rather than
    leaving the box stopped until the computer restarts.
    """

    session = _Session()
    signal.signal(signal.SIGINT, session.request_stop)
    signal.signal(signal.SIGTERM, session.request_stop)
    while True:
        session.restart.clear()
        try:
            _run_once(config, session)
        except Exception:
            if not _installed():
                raise
            _LOG.exception("the site box could not start; trying again in %.0f seconds", RETRY_SECONDS)
            if session.stopping.wait(RETRY_SECONDS):
                return 0
        else:
            if session.stopping.is_set() or not session.restart.is_set():
                return 0
            _LOG.info("restarting the site box as requested from the dashboard")
        if config_path is not None:
            try:
                config = load_local_config(config_path)
            except (OSError, ValueError) as error:
                _LOG.warning("could not read the config again; keeping the previous one: %s", error)


def _run_once(config: LocalConfig, session: _Session) -> None:
    """Start everything, run until stopped or restarted, then stop it all."""

    cleanup: list[Any] = []
    try:
        _serve(config, session, cleanup)
    finally:
        session.detach()
        for action in reversed(cleanup):
            try:
                action()
            except Exception:  # stopping one part must not block the others
                _LOG.exception("stopping a part of the site box failed")


def _serve(config: LocalConfig, session: _Session, cleanup: list[Any]) -> None:
    from .awake import KeepAwake
    from .capture import StreamSet
    from .live import LiveRelay
    from .setup_agent import SetupAgent, vault_camera_sources
    from .vault import BoxVault

    if _installed():
        from .model_download import fetch_model

        # Checks the pinned hash every start and replaces a missing or damaged
        # file; a failed download is retried by command_run.
        if fetch_model(config.detector_model):
            _LOG.info("detection model downloaded and verified")
    client = BackendClient(config.backend_url, config.site_key)
    detector = _detector(config)
    vault = BoxVault(config.vault) if config.vault is not None else None
    streams = StreamSet()

    def _stop_streams() -> None:
        for stream in streams.values():
            stream.stop()
        # Wait for each camera to let go of its connection (an RTSP read can
        # block for up to 10 seconds) so a restart can reconnect cleanly.
        for stream in streams.values():
            if isinstance(stream, threading.Thread) and stream.is_alive():
                stream.join(timeout=12)

    cleanup.append(_stop_streams)
    # _start_streams has already started these; a thread can start only once.
    for camera_id, stream in _start_streams(config).items():
        streams.add(camera_id, stream, start=False)
    if vault is not None:
        for camera_id, source in vault_camera_sources(vault).items():
            streams.add(camera_id, CameraStream(camera_id, source))
    awake = KeepAwake(enabled=config.keep_awake)
    awake.start()
    cleanup.append(awake.stop)
    worker = SiteWorker(
        backend=client,
        detector=detector,
        streams=streams,
        spool=Spool(config.spool_dir),
        fps=config.fps,
        min_confidence=config.min_confidence,
        processors=_processors(config, client),
        live_view=config.live_view,
    )

    # One poll carries both live-view requests and "Find cameras" tasks.
    agent = None
    if vault is not None and vault.exists():
        from functools import partial

        from .discovery import grab_thumbnail
        from .setup_agent import scene_counter

        # Pictures taken by Find cameras also report vehicles and people, so the
        # dashboard can suggest "Car park watch".
        agent = SetupAgent(client, vault, streams, thumbnail=partial(grab_thumbnail, inspect=scene_counter(detector)))

    def _tasks(tasks: list[Any]) -> None:
        route_tasks(tasks, client, session, agent)

    relay = LiveRelay(client, streams, on_tasks=_tasks, send_frames=config.live_view)
    relay.start()
    cleanup.append(relay.stop)
    if config.live_view:
        _LOG.info("live view available: frames are sent only while someone watches in the dashboard")
    else:
        _LOG.info("live view disabled in the site config")
    if agent is None:
        _LOG.info("Find cameras is off: pair this box with a code from the dashboard to use it")

    updater = None
    if config.auto_update and _installed():
        from .updater import Updater

        updates = (config.vault.parent if config.vault is not None else config.spool_dir.parent) / "updates"
        updater = Updater(VERSION, updates, install=sys.platform == "win32")
        updater.start()
        cleanup.append(updater.stop)

    def _status() -> dict[str, Any]:
        return {
            "version": VERSION,
            "paired": True,
            "find_cameras": agent is not None,
            "live_view": config.live_view,
            "keeps_awake": awake.active,
            "worker": worker.status(),
            "update": updater.state() if updater else {"message": f"Version {VERSION}. Automatic updates run only in the installed box."},
        }

    status_server = _status_server(config.status_port, _status)
    if status_server is not None:
        cleanup.append(status_server.stop)

    def _stop() -> None:
        _LOG.info("stopping site worker")
        relay.stop()
        worker.stop()

    session.attach(_stop)
    if not len(streams):
        _LOG.info("no cameras yet: add them with Find cameras in the dashboard (Configuration > Cameras)")
    _LOG.info("site worker running with %d camera(s) at %.1f fps", len(streams), config.fps)
    worker.run()


def _logging(verbose: bool, log_file: Path | None) -> None:
    handlers: list[logging.Handler] = [logging.StreamHandler()]
    log_error: OSError | None = None
    if log_file is not None:
        try:
            log_file.parent.mkdir(parents=True, exist_ok=True)
            handlers.append(logging.handlers.RotatingFileHandler(log_file, maxBytes=5 * 1024 * 1024, backupCount=3, encoding="utf-8"))
        except OSError as error:
            # A log file that cannot be opened must not stop the box.
            log_error = error
    logging.basicConfig(level=logging.DEBUG if verbose else logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s", handlers=handlers)
    if log_error is not None:
        _LOG.warning("could not open the log file %s: %s", log_file, log_error)


def main(argv: list[str] | None = None) -> int:
    from .home import DEFAULT_BACKEND, default_config_path

    parser = argparse.ArgumentParser(prog="syncam-box" if _installed() else "python -m syncam_ai.site_worker", description="SyncCam AI site box")
    parser.add_argument("--version", action="version", version=f"SyncCam Box {VERSION}")
    parser.add_argument(
        "command",
        choices=["run", "check", "snapshot", "enroll", "forget", "face-key", "liveness", "pair", "setup", "fetch-models", "update-check", "selftest"],
    )
    parser.add_argument("--config", type=Path, help="site JSON file (default: the box folder's syncam-site.json)")
    parser.add_argument("--code-file", type=Path, help="file holding the pairing code (setup)")
    parser.add_argument("--backend", default=DEFAULT_BACKEND, help="SyncCam API for a new box folder (setup)")
    parser.add_argument("--log-file", type=Path, help="also write logs to this file (rotated at 5 MB)")
    parser.add_argument("--camera", help="camera ID for the snapshot command")
    parser.add_argument("--out", type=Path, default=Path("snapshot.jpg"))
    parser.add_argument("--staff-id", help="staff ID from the dashboard for enroll/forget")
    parser.add_argument("--photos", type=Path, help="folder of the staff member's face photos for enroll")
    parser.add_argument("--seconds", type=float, default=30.0, help="how long the liveness command runs")
    parser.add_argument("--code", help="pairing code from the dashboard for the pair command")
    parser.add_argument("--name", help="name for this box in the dashboard (default: the computer name)")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args(argv)
    _logging(args.verbose, args.log_file)
    if args.command == "selftest":
        return command_selftest()
    if args.command == "update-check":
        return command_update_check()
    if args.command == "setup":
        code = args.code
        if args.code_file is not None:
            try:
                code = args.code_file.read_text(encoding="utf-8").strip()
            except OSError as error:
                _LOG.error("could not read the pairing code file: %s", error)
                return 2
        return command_setup(code or None, args.name, args.backend)
    if args.config is None:
        args.config = default_config_path()
    if args.command == "fetch-models":
        return command_fetch_models(args.config)
    if args.command == "face-key":
        from .faces import FaceGallery

        print("Store this key as the SYNCAM_FACE_KEY environment variable on this site box only.")
        print("Losing it means re-enrolling staff; never copy it to the cloud or into Git.")
        print(FaceGallery.new_key())
        return 0
    if args.command == "pair":
        if not args.code:
            parser.error("--code is required for pair")
        return command_pair(args.config, args.code, args.name)
    try:
        config = load_local_config(args.config)
    except NotPairedError as error:
        if args.command != "run" or not _installed():
            print(f"config error: {error}", file=sys.stderr)
            return 2
        config = wait_until_paired(args.config)
    except (OSError, ValueError) as error:
        if args.command == "run" and _installed():
            # Logged and shown on the status page; retried until it is fixed.
            config = wait_until_config_readable(args.config, error)
        else:
            _LOG.error("config error: %s", error)
            print(f"config error: {error}", file=sys.stderr)
            return 2
    if args.command == "check":
        return command_check(config)
    if args.command == "snapshot":
        if not args.camera:
            parser.error("--camera is required for snapshot")
        return command_snapshot(config, args.camera, args.out)
    if args.command == "liveness":
        return command_liveness(config, args.camera, args.seconds)
    if args.command in {"enroll", "forget"}:
        if not args.staff_id:
            parser.error("--staff-id is required")
        if args.command == "forget":
            return command_forget(config, args.staff_id)
        if args.photos is None or not args.photos.is_dir():
            parser.error("--photos must be a folder of face photos")
        return command_enroll(config, args.staff_id, args.photos)
    return command_run(config, args.config)


if __name__ == "__main__":
    raise SystemExit(main())
