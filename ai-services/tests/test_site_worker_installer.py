"""The installed site box: box folder, model download, status page, signed updates, setup."""

import base64
import contextlib
import hashlib
import http.client
import importlib.util
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest
from unittest import mock

SRC = pathlib.Path(__file__).parents[1] / "src"
ROOT = pathlib.Path(__file__).parents[2]
sys.path.insert(0, str(SRC))

from syncam_ai.model_artifacts import ModelArtifact  # noqa: E402
from syncam_ai.site_worker import home, model_download, status  # noqa: E402
from syncam_ai.site_worker.config import NotPairedError, parse_local_config  # noqa: E402

HAS_CRYPTO = importlib.util.find_spec("cryptography") is not None
REPO = "https://github.com/syncaitechnologies/SyncamAi/releases/download/"


class HomeTest(unittest.TestCase):
    def test_location(self) -> None:
        self.assertEqual(home.box_home({"SYNCAM_HOME": "/srv/box"}), pathlib.Path("/srv/box"))
        if os.name == "nt":
            self.assertEqual(home.box_home({"PROGRAMDATA": r"D:\Data"}), pathlib.Path(r"D:\Data\SyncCam"))
        self.assertEqual(home.default_config_path({"SYNCAM_HOME": "/srv/box"}).name, "syncam-site.json")

    def test_new_box_folder_has_a_working_config_without_cameras(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            box = pathlib.Path(directory) / "SyncCam"
            config_path = home.ensure_home(box)
            for folder in ("models", "spool", "logs", "updates"):
                self.assertTrue((box / folder).is_dir())
            raw = json.loads(config_path.read_text(encoding="utf-8"))
            self.assertNotIn("site_key", raw, "no secret is ever written to the config")
            config = parse_local_config(raw, environ={"SYNCAM_SITE_KEY": "sck_x"})
            self.assertEqual(config.cameras, {})
            self.assertEqual(config.detector_model, box / "models" / "yolox_s.onnx")
            self.assertEqual((config.status_port, config.auto_update), (8790, True))
            config_path.write_text(json.dumps({**raw, "fps": 2}), encoding="utf-8")
            home.ensure_home(box, "https://other.example")
            self.assertEqual(json.loads(config_path.read_text(encoding="utf-8"))["fps"], 2, "an existing config is kept")
            with self.assertRaises(ValueError):
                home.ensure_home(pathlib.Path(directory) / "other", "http://api.example")

    def test_status_port_and_pairing_errors(self) -> None:
        raw = {"backend_url": "https://api.example", "models": {"detector": "m.onnx"}}
        with self.assertRaises(NotPairedError):
            parse_local_config(raw, environ={})
        with self.assertRaises(ValueError):
            parse_local_config({**raw, "status_port": 80}, environ={"SYNCAM_SITE_KEY": "k"})
        self.assertEqual(parse_local_config({**raw, "status_port": 0, "auto_update": False}, environ={"SYNCAM_SITE_KEY": "k"}).status_port, 0)


class ModelDownloadTest(unittest.TestCase):
    def setUp(self) -> None:
        self.artifact = ModelArtifact("test-model", "Apache-2.0", "https://models.example/m.onnx", (("onnx", hashlib.sha384(b"weights").hexdigest()),))
        self.calls: list[str] = []

    def _download(self, body: bytes):
        def download(url: str, target: pathlib.Path) -> None:
            self.calls.append(url)
            target.write_bytes(body)

        return download

    def test_downloads_once_and_keeps_only_matching_files(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            target = pathlib.Path(directory) / "models" / "m.onnx"
            self.assertTrue(model_download.fetch_model(target, self.artifact, download=self._download(b"weights")))
            self.assertFalse(model_download.fetch_model(target, self.artifact, download=self._download(b"weights")))
            self.assertEqual(self.calls, ["https://models.example/m.onnx"])
            target.write_bytes(b"corrupted")
            with self.assertRaises(ValueError):
                model_download.fetch_model(target, self.artifact, download=self._download(b"tampered"))
            self.assertEqual(sorted(path.name for path in target.parent.iterdir()), ["m.onnx"], "no partial file is left")
            self.assertEqual(target.read_bytes(), b"corrupted", "a bad download never replaces the file")

    def test_https_only(self) -> None:
        with tempfile.TemporaryDirectory() as directory, self.assertRaises(ValueError):
            model_download.download_file("http://models.example/m.onnx", pathlib.Path(directory) / "m")


class StatusPageTest(unittest.TestCase):
    def setUp(self) -> None:
        self.payload = {
            "version": "0.1.0",
            "paired": True,
            "worker": {
                "site": "Main <office>",
                "connected": True,
                "connected_at": "2026-11-16T10:00:00+00:00",
                "cameras": [{"id": "c1", "name": "<b>Gate</b>", "last_frame_seconds": 1.2}, {"id": "c2", "name": "", "last_frame_seconds": None}],
                "alerts_queued": 2,
                "alerts_pending": 0,
                "last_alert_at": None,
            },
            "update": {"message": "Version 0.1.0 is up to date."},
        }
        self.server = status.StatusServer(lambda: self.payload, port=0)
        self.server.start()
        self.addCleanup(self.server.stop)

    def _get(self, path: str, host: str | None = None) -> tuple[int, str, dict]:
        connection = http.client.HTTPConnection("127.0.0.1", self.server.port, timeout=5)
        connection.request("GET", path, headers={"Host": host or f"127.0.0.1:{self.server.port}"})
        response = connection.getresponse()
        body = response.read().decode()
        connection.close()
        return response.status, body, dict(response.getheaders())

    def test_page_and_json(self) -> None:
        code, body, headers = self._get("/")
        self.assertEqual(code, 200)
        self.assertIn("&lt;b&gt;Gate&lt;/b&gt;", body, "camera names are escaped")
        self.assertIn("Main &lt;office&gt;", body)
        self.assertIn("frame 1 s ago", body)
        self.assertIn("no frames yet", body)
        self.assertIn("default-src 'none'", headers["Content-Security-Policy"])
        code, body, _ = self._get("/status.json")
        self.assertEqual((code, json.loads(body)["worker"]["alerts_queued"]), (200, 2))

    def test_refuses_other_hosts_and_paths(self) -> None:
        self.assertEqual(self._get("/", host="attacker.example")[0], 403, "DNS rebinding is refused")
        self.assertEqual(self._get("/", host=f"attacker.example:{self.server.port}")[0], 403)
        self.assertEqual(self._get("/", host=f"localhost:{self.server.port}")[0], 200)
        self.assertEqual(self._get("/vault")[0], 404)
        self.payload = None  # the provider now fails
        self.assertEqual(self._get("/")[0], 500)

    def test_loopback_only_and_unpaired_hint(self) -> None:
        with self.assertRaises(ValueError):
            status.StatusServer(dict, port=0, host="0.0.0.0")
        page = status.render_status_html({"version": "0.1.0", "paired": False, "worker": {}, "update": {}})
        self.assertIn("pair --code", page)
        self.assertIn("Find cameras", page)
        offline = status.render_status_html({"paired": True, "worker": {"connected": False, "connected_at": "2026-11-16T10:00:00+00:00"}})
        self.assertIn("&#10007; Connected to SyncCam", offline, "an old connection is not shown as connected")
        self.assertIn("Last reached SyncCam at 2026-11-16T10:00:00+00:00", offline)


def _keys(seed: int):
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

    private = Ed25519PrivateKey.from_private_bytes(bytes([seed]) * 32)
    public = private.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo).decode()
    return private, public


def _manifest(private, **overrides) -> tuple[bytes, bytes]:
    data = {"version": "0.2.0", "url": REPO + "box-v0.2.0/SyncCamSetup-0.2.0.exe", "sha256": hashlib.sha256(b"installer").hexdigest(), **overrides}
    body = json.dumps(data).encode()
    return body, base64.b64encode(private.sign(body))


@unittest.skipUnless(HAS_CRYPTO, "cryptography is needed for signed updates")
class UpdaterTest(unittest.TestCase):
    def setUp(self) -> None:
        from syncam_ai.site_worker import updater

        self.updater = updater
        self.private, self.public = _keys(7)
        self.other, self.other_public = _keys(9)

    def test_versions(self) -> None:
        self.assertEqual(self.updater.parse_version("1.20.3"), (1, 20, 3))
        for bad in ("1.2", "01.2.3", "1.2.3-beta", "v1.2.3", ""):
            with self.assertRaises(ValueError):
                self.updater.parse_version(bad)

    def test_signed_manifest(self) -> None:
        body, signature = _manifest(self.private)
        info = self.updater.verify_manifest(body, signature, (self.other_public, self.public))
        self.assertEqual((info.version, info.url.rsplit("/", 1)[-1]), ("0.2.0", "SyncCamSetup-0.2.0.exe"))
        with self.assertRaises(ValueError):
            self.updater.verify_manifest(body, signature, (self.other_public,))
        with self.assertRaises(ValueError):
            self.updater.verify_manifest(body.replace(b"0.2.0", b"9.2.0"), signature, (self.public,))
        with self.assertRaises(ValueError):
            self.updater.verify_manifest(body, b"not base64!", (self.public,))
        for overrides in (
            {"url": "https://evil.example/SyncCamSetup-0.2.0.exe"},
            {"url": REPO + "box-v0.2.0/other.exe"},
            {"url": REPO + "box-v0.2.0/../../x/SyncCamSetup-0.2.0.exe"},
            {"sha256": "abc"},
            {"version": "0.2"},
        ):
            bad, bad_signature = _manifest(self.private, **overrides)
            with self.assertRaises(ValueError, msg=str(overrides)):
                self.updater.verify_manifest(bad, bad_signature, (self.public,))

    def test_check_only_offers_newer_versions(self) -> None:
        body, signature = _manifest(self.private)
        fetched = {self.updater.MANIFEST_URL: body, self.updater.MANIFEST_URL + ".sig": signature}

        def fetch(url, limit):
            return fetched[url]

        self.assertEqual(self.updater.check_for_update("0.1.9", fetch=fetch, public_keys=(self.public,)).version, "0.2.0")
        self.assertIsNone(self.updater.check_for_update("0.2.0", fetch=fetch, public_keys=(self.public,)))
        self.assertIsNone(self.updater.check_for_update("1.0.0", fetch=fetch, public_keys=(self.public,)), "no downgrades")
        with self.assertRaises(self.updater.UpdatesDisabled):
            self.updater.check_for_update("0.1.0", fetch=fetch, public_keys=())

    def test_download_checks_the_signed_hash(self) -> None:
        info = self.updater.UpdateInfo("0.2.0", REPO + "box-v0.2.0/SyncCamSetup-0.2.0.exe", hashlib.sha256(b"installer").hexdigest())
        with tempfile.TemporaryDirectory() as directory:
            folder = pathlib.Path(directory)
            with self.assertRaises(ValueError):
                self.updater.download_installer(info, folder, fetch=lambda url, limit: b"tampered")
            self.assertEqual(list(folder.iterdir()), [])
            path = self.updater.download_installer(info, folder, fetch=lambda url, limit: b"installer")
            self.assertEqual((path.name, path.read_bytes()), ("SyncCamSetup-0.2.0.exe", b"installer"))

    def test_installer_runs_as_a_separate_system_task(self) -> None:
        import xml.etree.ElementTree as ElementTree

        calls, definitions = [], []

        def run(args, **kwargs):
            calls.append(args)
            if "/XML" in args:
                definitions.append(pathlib.Path(args[args.index("/XML") + 1]).read_text(encoding="utf-16"))

        with tempfile.TemporaryDirectory() as directory:
            installer = pathlib.Path(directory) / "Sync & Cam" / "SyncCamSetup-0.2.0.exe"
            installer.parent.mkdir()
            self.updater.schedule_installer(installer, installer.parent / "install.log", run=run)
            self.assertEqual(list(installer.parent.iterdir()), [], "the task definition file is removed")
        create, start = calls
        self.assertEqual(create[:4], ["schtasks", "/Create", "/TN", self.updater.UPDATE_TASK])
        self.assertNotIn("/ST", create, "no start time that can be in the past")
        self.assertEqual(start, ["schtasks", "/Run", "/TN", self.updater.UPDATE_TASK])
        namespace = {"t": "http://schemas.microsoft.com/windows/2004/02/mit/task"}
        task = ElementTree.fromstring(definitions[0].split("\n", 1)[1])
        self.assertIsNone(task.find("t:Triggers", namespace), "it runs only when started")
        self.assertEqual(task.findtext("t:Principals/t:Principal/t:UserId", namespaces=namespace), "S-1-5-18")
        self.assertEqual(task.findtext("t:Actions/t:Exec/t:Command", namespaces=namespace), str(installer))
        self.assertIn("/VERYSILENT", task.findtext("t:Actions/t:Exec/t:Arguments", namespaces=namespace))

    def test_updater_states(self) -> None:
        info = self.updater.UpdateInfo("0.2.0", REPO + "box-v0.2.0/SyncCamSetup-0.2.0.exe", "0" * 64)
        launched = []

        def make(check, install=True, download=lambda info, folder: folder / "setup.exe"):
            return self.updater.Updater("0.1.0", pathlib.Path("updates"), install=install, check=check, download=download, launch=lambda path, log: launched.append(path))

        current = make(lambda version: None)
        current.check_once()
        self.assertIn("up to date", current.state()["message"])
        disabled = make(lambda version: (_ for _ in ()).throw(self.updater.UpdatesDisabled("no release key")))
        disabled.check_once()
        self.assertIn("off", disabled.state()["message"])
        failing = make(lambda version: (_ for _ in ()).throw(OSError("offline")))
        failing.check_once()
        self.assertIn("retried", failing.state()["message"])
        development = make(lambda version: info, install=False)
        development.check_once()
        self.assertEqual((development.state()["latest"], launched), ("0.2.0", []))
        installed = make(lambda version: info)
        installed.check_once()
        self.assertEqual(launched, [pathlib.Path("updates") / "setup.exe"])
        broken = make(lambda version: info, download=lambda info, folder: (_ for _ in ()).throw(ValueError("hash")))
        broken.check_once()
        self.assertIn("could not be installed", broken.state()["message"])
        stopped = make(lambda version: None)
        stopped.stop()
        stopped.run()  # returns at once when stopped

    def test_built_in_key_is_ed25519(self) -> None:
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
        from cryptography.hazmat.primitives.serialization import load_pem_public_key

        from syncam_ai.site_worker.release_keys import RELEASE_PUBLIC_KEYS

        self.assertTrue(RELEASE_PUBLIC_KEYS)
        for pem in RELEASE_PUBLIC_KEYS:
            self.assertIsInstance(load_pem_public_key(pem.encode()), Ed25519PublicKey)

    def test_release_tool_signs_what_boxes_accept(self) -> None:
        from cryptography.hazmat.primitives import serialization

        spec = importlib.util.spec_from_file_location("release_manifest", ROOT / "installer" / "release_manifest.py")
        tool = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(tool)
        pem = self.private.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()).decode()
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ, {"SYNCAM_RELEASE_SIGNING_KEY": pem}):
            folder = pathlib.Path(directory)
            installer = folder / "SyncCamSetup-0.2.0.exe"
            installer.write_bytes(b"installer")
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(tool.main(["sign", "--installer", str(installer), "--version", "0.2.0", "--tag", "box-v0.2.0", "--out", str(folder)]), 0)
                self.assertEqual(tool.main(["sign", "--installer", str(installer), "--version", "0.2.0", "--tag", "box-v0.3.0", "--out", str(folder)]), 1)
            body = (folder / "syncam-box-manifest.json").read_bytes()
            info = self.updater.verify_manifest(body, (folder / "syncam-box-manifest.json.sig").read_bytes(), (self.public,))
            self.assertEqual(info.sha256, hashlib.sha256(b"installer").hexdigest())
            self.assertEqual(info.url, REPO + "box-v0.2.0/SyncCamSetup-0.2.0.exe")


class SetupCommandTest(unittest.TestCase):
    def setUp(self) -> None:
        from syncam_ai.site_worker import __main__ as cli

        self.cli = cli
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.home = pathlib.Path(self.directory.name) / "SyncCam"
        patcher = mock.patch.dict(os.environ, {"SYNCAM_HOME": str(self.home)})
        patcher.start()
        self.addCleanup(patcher.stop)
        self.fetch = mock.patch("syncam_ai.site_worker.model_download.fetch_model", return_value=True).start()
        self.pair = mock.patch("syncam_ai.site_worker.pairing.pair_box", return_value=mock.Mock(site_id="site-1")).start()
        self.addCleanup(mock.patch.stopall)

    def _main(self, *args: str) -> int:
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return self.cli.main(list(args))

    def test_setup_creates_the_folder_downloads_and_pairs_from_a_code_file(self) -> None:
        code_file = pathlib.Path(self.directory.name) / "code.txt"
        code_file.write_text(" K7Q4-XM92 \n", encoding="utf-8")
        self.assertEqual(self._main("setup", "--code-file", str(code_file), "--name", "Front desk PC"), 0)
        self.assertTrue((self.home / "syncam-site.json").is_file())
        self.fetch.assert_called_once_with(self.home / "models" / "yolox_s.onnx")
        vault, backend, code = self.pair.call_args.args
        self.assertEqual((vault.path, backend, code), (self.home / "syncam-box.vault", home.DEFAULT_BACKEND, "K7Q4-XM92"))
        self.assertEqual(self.pair.call_args.kwargs, {"label": "Front desk PC"})

    def test_setup_without_code_or_already_paired_does_not_pair(self) -> None:
        self.assertEqual(self._main("setup"), 0)
        (self.home / "syncam-box.vault").write_bytes(b"x")
        self.assertEqual(self._main("setup", "--code", "K7Q4XM92"), 0)
        self.pair.assert_not_called()

    def test_setup_reports_failures(self) -> None:
        from syncam_ai.site_worker.client import BackendError

        self.pair.side_effect = BackendError("code expired", status=404, retryable=False)
        self.assertEqual(self._main("setup", "--code", "K7Q4XM92"), 1)
        self.pair.side_effect = None
        self.fetch.side_effect = ValueError("checksum")
        self.assertEqual(self._main("setup", "--code", "K7Q4XM92"), 1)
        self.assertEqual(self._main("setup", "--code-file", str(self.home / "missing.txt")), 2)

    def test_version_and_default_config(self) -> None:
        with self.assertRaises(SystemExit) as exit_code, contextlib.redirect_stdout(io.StringIO()) as output:
            self.cli.main(["--version"])
        self.assertEqual(exit_code.exception.code, 0)
        self.assertIn("SyncCam Box", output.getvalue())
        self.assertEqual(self._main("check"), 2, "the default config of an unset-up box is missing")

    def test_installed_box_rechecks_the_model_every_start(self) -> None:
        config = mock.Mock(detector_model=self.home / "models" / "yolox_s.onnx", backend_url="https://api.example", site_key="sck_x")
        # A failed start is retried; the second attempt is ended by Ctrl+C.
        with mock.patch.object(self.cli, "_installed", return_value=True), mock.patch.object(self.cli, "RETRY_SECONDS", 0), \
                mock.patch.object(self.cli, "_detector", side_effect=[RuntimeError("damaged model"), KeyboardInterrupt()]), \
                mock.patch.object(self.cli.signal, "signal"):
            with self.assertRaises(KeyboardInterrupt), self.assertLogs("syncam.site_worker", "ERROR"):
                self.cli.command_run(config)
        self.assertEqual(self.fetch.call_args_list, [mock.call(self.home / "models" / "yolox_s.onnx")] * 2)

    def test_unpaired_installed_box_waits_for_pairing(self) -> None:
        home.ensure_home(self.home)
        ready = mock.Mock(name="config")
        with mock.patch.object(self.cli, "load_local_config", side_effect=[NotPairedError("not paired"), ready]), mock.patch.object(self.cli.time, "sleep"):
            raw = json.loads((self.home / "syncam-site.json").read_text(encoding="utf-8"))
            (self.home / "syncam-site.json").write_text(json.dumps({**raw, "status_port": 0}), encoding="utf-8")
            self.assertIs(self.cli.wait_until_paired(self.home / "syncam-site.json"), ready)


if __name__ == "__main__":
    unittest.main()
