"""An installed site box whose settings cannot be read must say so, not vanish.

A reinstall used to leave the box's files with no permissions at all; the
box then quit at every start with an empty log and the status page showed
"can't be reached". It now logs the reason, keeps the status page up with it
and starts as soon as the settings can be read again.
"""

from __future__ import annotations

import json
import pathlib
import sys
import tempfile
import threading
import unittest
from unittest import mock

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker import __main__ as cli  # noqa: E402
from syncam_ai.site_worker.status import render_status_html  # noqa: E402


def write_config(path: pathlib.Path) -> None:
    path.write_text(json.dumps({
        "backend_url": "https://api.example.test",
        "site_key": "sck_test_placeholder",
        "models": {"detector": str(path.parent / "yolox_s.onnx")},
        "spool_dir": str(path.parent / "spool"),
        "vault": str(path.parent / "syncam-box.vault"),
    }), encoding="utf-8")


class UnreadableSettingsTest(unittest.TestCase):
    def test_the_box_waits_and_starts_once_the_settings_are_readable(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "syncam-site.json"
            path.write_text("{not json", encoding="utf-8")
            fixed = threading.Timer(0.1, write_config, args=(path,))
            fixed.start()
            self.addCleanup(fixed.cancel)
            with self.assertLogs("syncam.site_worker", level="ERROR") as logs:
                config = cli.wait_until_config_readable(path, PermissionError(13, "Permission denied"), poll_seconds=0.02, port=0)
            self.assertEqual(config.backend_url, "https://api.example.test")
            self.assertIn("cannot read its settings", "\n".join(logs.output), "the reason is written to the log")

    def test_main_keeps_an_installed_box_alive_and_reports_elsewhere(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "syncam-site.json"
            path.write_text("{not json", encoding="utf-8")
            sentinel = object()
            with mock.patch.object(cli, "_installed", return_value=True), \
                    mock.patch.object(cli, "wait_until_config_readable", return_value=sentinel) as waits, \
                    mock.patch.object(cli, "command_run", return_value=0) as runs:
                self.assertEqual(cli.main(["run", "--config", str(path)]), 0)
            waits.assert_called_once()
            self.assertIs(runs.call_args.args[0], sentinel)
            with mock.patch.object(cli, "_installed", return_value=False), self.assertLogs("syncam.site_worker", level="ERROR"):
                self.assertEqual(cli.main(["run", "--config", str(path)]), 2, "a developer run still exits with the error")

    def test_a_log_file_that_cannot_be_opened_does_not_stop_the_box(self) -> None:
        with mock.patch("logging.handlers.RotatingFileHandler", side_effect=PermissionError(13, "Permission denied")), \
                mock.patch("logging.basicConfig"):
            cli._logging(False, pathlib.Path(tempfile.gettempdir()) / "syncam-test-logs" / "syncam-box.log")

    def test_the_status_page_shows_the_problem(self) -> None:
        page = render_status_html({"version": "0.1.6", "paired": None, "worker": {}, "problem": "This box cannot read its settings (<denied>)."})
        self.assertIn("Site box started", page)
        self.assertIn("cannot read its settings (&lt;denied&gt;)", page)
        self.assertNotIn("Paired with SyncCam", page, "pairing is unknown while the settings cannot be read")
        self.assertIn("Paired with SyncCam", render_status_html({"paired": True, "worker": {}}))


class InstallerLockTest(unittest.TestCase):
    def test_the_installer_never_grants_recursively(self) -> None:
        # icacls ignores an (OI)(CI) grant on a file but still strips its
        # inherited permissions, so "/grant ... /T" locked every existing
        # file of a reinstalled box. Grant on the folder, then /reset inside.
        script = (pathlib.Path(__file__).parents[2] / "installer" / "configure-box.ps1").read_text(encoding="utf-8")
        commands = [line for line in script.splitlines() if "icacls" in line and not line.lstrip().startswith("#")]
        self.assertTrue(any("/grant:r" in line for line in commands))
        for line in commands:
            self.assertFalse("/grant" in line and " /T" in line, line)
        self.assertTrue(any("/reset /T" in line for line in commands), "files already inside take the folder's permissions")


if __name__ == "__main__":
    unittest.main()
