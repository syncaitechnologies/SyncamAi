"""Remote restart from the dashboard and keeping the site box's PC awake."""

from __future__ import annotations

import pathlib
import sys
import unittest
from unittest import mock

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker import __main__ as cli  # noqa: E402
from syncam_ai.site_worker import status  # noqa: E402
from syncam_ai.site_worker.awake import (  # noqa: E402
    ES_CONTINUOUS,
    ES_SYSTEM_REQUIRED,
    INVALID_HANDLE_VALUE,
    POWER_REQUEST_SYSTEM_REQUIRED,
    KeepAwake,
)
from syncam_ai.site_worker.client import BackendError  # noqa: E402
from syncam_ai.site_worker.config import parse_local_config  # noqa: E402


class _Kernel32:
    def __init__(self, *, handle: object = 101, set_ok: int = 1, thread_ok: int = 1) -> None:
        self.calls: list[tuple] = []
        self._handle, self._set_ok, self._thread_ok = handle, set_ok, thread_ok

    def PowerCreateRequest(self, context: object) -> object:  # noqa: N802 - Windows name
        self.calls.append(("create",))
        return self._handle

    def PowerSetRequest(self, handle: object, kind: int) -> int:  # noqa: N802
        self.calls.append(("set", handle, kind))
        return self._set_ok

    def PowerClearRequest(self, handle: object, kind: int) -> int:  # noqa: N802
        self.calls.append(("clear", handle, kind))
        return 1

    def CloseHandle(self, handle: object) -> int:  # noqa: N802
        self.calls.append(("close", handle))
        return 1

    def SetThreadExecutionState(self, flags: int) -> int:  # noqa: N802
        self.calls.append(("thread", flags))
        return self._thread_ok


class KeepAwakeTest(unittest.TestCase):
    def test_holds_a_system_required_request_until_stopped(self) -> None:
        kernel32 = _Kernel32()
        awake = KeepAwake(platform="win32", kernel32=kernel32)
        self.assertTrue(awake.start())
        self.assertTrue(awake.active)
        self.assertTrue(awake.start(), "starting twice keeps one request")
        self.assertEqual(kernel32.calls, [("create",), ("set", 101, POWER_REQUEST_SYSTEM_REQUIRED)])
        awake.stop()
        self.assertFalse(awake.active)
        self.assertEqual(kernel32.calls[-2:], [("clear", 101, POWER_REQUEST_SYSTEM_REQUIRED), ("close", 101)])

    def test_falls_back_to_the_thread_setting(self) -> None:
        kernel32 = _Kernel32(handle=INVALID_HANDLE_VALUE)
        awake = KeepAwake(platform="win32", kernel32=kernel32)
        self.assertTrue(awake.start())
        self.assertIn(("thread", ES_CONTINUOUS | ES_SYSTEM_REQUIRED), kernel32.calls)
        awake.stop()
        self.assertEqual(kernel32.calls[-1], ("thread", ES_CONTINUOUS))

    def test_refused_request_is_reported_not_raised(self) -> None:
        kernel32 = _Kernel32(set_ok=0, thread_ok=0)
        awake = KeepAwake(platform="win32", kernel32=kernel32)
        with self.assertLogs("syncam_ai.site_worker.awake", "WARNING"):
            self.assertFalse(awake.start())
        self.assertIn(("close", 101), kernel32.calls, "a request Windows did not accept is closed")
        self.assertFalse(awake.active)

    def test_off_when_disabled_or_not_windows(self) -> None:
        kernel32 = _Kernel32()
        self.assertFalse(KeepAwake(enabled=False, platform="win32", kernel32=kernel32).start())
        self.assertFalse(KeepAwake(platform="linux", kernel32=kernel32).start())
        self.assertEqual(kernel32.calls, [])
        KeepAwake(platform="linux").stop()

    def test_config_switch_and_status_page(self) -> None:
        raw = {"backend_url": "https://api.example", "models": {"detector": "m.onnx"}}
        self.assertTrue(parse_local_config(raw, environ={"SYNCAM_SITE_KEY": "sck_x"}).keep_awake)
        self.assertFalse(parse_local_config({**raw, "keep_awake": False}, environ={"SYNCAM_SITE_KEY": "sck_x"}).keep_awake)
        page = status.render_status_html({"paired": True, "keeps_awake": False, "worker": {"connected": True}})
        self.assertIn("Keeping this computer awake", page)
        self.assertIn("set Sleep to Never", page)
        self.assertNotIn("Keeping this computer awake", status.render_status_html({"paired": False, "worker": {}}))


class RouteTasksTest(unittest.TestCase):
    def test_restart_is_confirmed_then_requested_and_setup_tasks_still_run(self) -> None:
        client, agent, session = mock.Mock(), mock.Mock(), cli._Session()
        stop = mock.Mock()
        session.attach(stop)
        cli.route_tasks([{"id": "s1", "type": "scan"}, {"id": "r1", "type": "restart"}], client, session, agent)
        agent.handle.assert_called_once_with([{"id": "s1", "type": "scan"}])
        client.post_setup_result.assert_called_once_with("r1", {"status": "done", "result": {"restarting": True}})
        self.assertTrue(session.restart.is_set())
        stop.assert_called_once_with()

    def test_restart_works_without_find_cameras_and_when_the_confirmation_fails(self) -> None:
        client, session = mock.Mock(), cli._Session()
        client.post_setup_result.side_effect = BackendError("offline")
        with self.assertLogs("syncam.site_worker", "WARNING"):
            cli.route_tasks([{"id": "r1", "type": "restart"}], client, session, None)
        self.assertTrue(session.restart.is_set())

    def test_a_stop_before_the_run_attaches_is_not_lost(self) -> None:
        session = cli._Session()
        session.request_stop()
        stop = mock.Mock()
        session.attach(stop)
        stop.assert_called_once_with()


class RestartLoopTest(unittest.TestCase):
    def setUp(self) -> None:
        mock.patch.object(cli.signal, "signal").start()
        self.addCleanup(mock.patch.stopall)

    def test_a_dashboard_restart_runs_again_with_the_config_read_from_disk(self) -> None:
        first, reloaded = mock.Mock(name="first"), mock.Mock(name="reloaded")
        seen = []

        def run_once(config: object, session: cli._Session) -> None:
            seen.append(config)
            if len(seen) == 1:
                session.request_restart()

        with mock.patch.object(cli, "_run_once", side_effect=run_once), mock.patch.object(cli, "load_local_config", return_value=reloaded) as load:
            self.assertEqual(cli.command_run(first, pathlib.Path("site.json")), 0)
        self.assertEqual(seen, [first, reloaded])
        load.assert_called_once_with(pathlib.Path("site.json"))

    def test_a_stop_ends_the_loop_even_while_restarting(self) -> None:
        def run_once(config: object, session: cli._Session) -> None:
            session.request_restart()
            session.request_stop()

        with mock.patch.object(cli, "_run_once", side_effect=run_once) as run:
            self.assertEqual(cli.command_run(mock.Mock()), 0)
        self.assertEqual(run.call_count, 1)

    def test_an_unreadable_config_keeps_the_previous_one(self) -> None:
        config, seen = mock.Mock(), []

        def run_once(current: object, session: cli._Session) -> None:
            seen.append(current)
            if len(seen) == 1:
                session.request_restart()

        with mock.patch.object(cli, "_run_once", side_effect=run_once), mock.patch.object(cli, "load_local_config", side_effect=ValueError("bad json")):
            with self.assertLogs("syncam.site_worker", "WARNING"):
                cli.command_run(config, pathlib.Path("site.json"))
        self.assertEqual(seen, [config, config])

    def test_installed_box_retries_a_failed_start_and_a_dev_run_does_not(self) -> None:
        with mock.patch.object(cli, "_installed", return_value=True), mock.patch.object(cli, "RETRY_SECONDS", 0), \
                mock.patch.object(cli, "_run_once", side_effect=[OSError("no internet"), None]) as run:
            with self.assertLogs("syncam.site_worker", "ERROR"):
                self.assertEqual(cli.command_run(mock.Mock()), 0)
        self.assertEqual(run.call_count, 2)
        with mock.patch.object(cli, "_installed", return_value=False), mock.patch.object(cli, "_run_once", side_effect=OSError("no internet")):
            with self.assertRaises(OSError):
                cli.command_run(mock.Mock())

    def test_every_part_is_stopped_even_if_one_fails(self) -> None:
        stopped = []

        def serve(config: object, session: cli._Session, cleanup: list) -> None:
            cleanup.append(lambda: stopped.append("streams"))
            cleanup.append(lambda: (_ for _ in ()).throw(RuntimeError("stuck")))
            cleanup.append(lambda: stopped.append("status page"))
            raise RuntimeError("camera failed to start")

        with mock.patch.object(cli, "_serve", side_effect=serve):
            with self.assertLogs("syncam.site_worker", "ERROR"), self.assertRaises(RuntimeError):
                cli._run_once(mock.Mock(), cli._Session())
        self.assertEqual(stopped, ["status page", "streams"])


if __name__ == "__main__":
    unittest.main()
