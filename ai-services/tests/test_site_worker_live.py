import importlib.util
import pathlib
import sys
import unittest
from datetime import datetime, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.client import BackendError  # noqa: E402
from syncam_ai.site_worker.config import parse_local_config  # noqa: E402
from syncam_ai.site_worker.live import LiveRelay  # noqa: E402

CAMERA = "33333333-3333-4333-8333-333333333333"
OTHER = "44444444-4444-4444-8444-444444444444"
AT = datetime(2026, 11, 15, 18, 0, tzinfo=timezone.utc)


class _Stream:
    def __init__(self):
        self.sequence = 0

    def latest(self):
        self.sequence += 1
        return self.sequence, f"frame-{self.sequence}", AT


class _Backend:
    def __init__(self, wanted):
        self.wanted = list(wanted)
        self.polls = 0
        self.frames = []
        self.watching = True
        self.fail_poll = False

    def live_requests(self):
        self.polls += 1
        if self.fail_poll:
            raise BackendError("offline")
        return self.wanted

    def put_live_frame(self, camera_id, jpeg):
        self.frames.append((camera_id, jpeg))
        return self.watching


def relay(backend, clock, streams=None):
    return LiveRelay(backend, streams or {CAMERA: _Stream()}, encode=lambda frame, at: frame.encode(), clock=lambda: clock[0])


class LiveRelayTest(unittest.TestCase):
    def test_nothing_is_sent_while_nobody_watches(self) -> None:
        clock = [0.0]
        backend = _Backend([])
        live = relay(backend, clock)
        for _ in range(50):
            clock[0] += 0.1
            live.step()
        self.assertEqual(backend.frames, [])
        self.assertLessEqual(backend.polls, 3, "the wanted list is polled every 2 s, not every pass")

    def test_watched_camera_gets_about_two_frames_a_second(self) -> None:
        clock = [0.0]
        backend = _Backend([CAMERA])
        live = relay(backend, clock)
        for _ in range(30):  # 3 seconds of 0.1 s passes
            clock[0] += 0.1
            live.step()
        self.assertIn(len(backend.frames), (6, 7))
        self.assertTrue(all(camera == CAMERA for camera, _ in backend.frames))

    def test_stops_when_the_api_says_nobody_watches(self) -> None:
        clock = [0.0]
        backend = _Backend([CAMERA])
        live = relay(backend, clock)
        live.step()
        backend.watching = False
        clock[0] += 0.6
        live.step()
        sent = len(backend.frames)
        backend.wanted = []
        for _ in range(10):
            clock[0] += 0.1
            live.step()
        self.assertEqual(len(backend.frames), sent, "no frames after the session ended")
        self.assertEqual(live.wanted, frozenset())

    def test_unknown_cameras_are_ignored_and_poll_failures_keep_running(self) -> None:
        clock = [0.0]
        backend = _Backend([OTHER])
        live = relay(backend, clock)
        live.step()
        self.assertEqual((live.wanted, backend.frames), (frozenset(), []))
        backend.fail_poll = True
        clock[0] += 3
        live.step()
        self.assertEqual(backend.frames, [])

    def test_the_same_frame_is_not_sent_twice(self) -> None:
        class _Frozen:
            def latest(self):
                return 7, "same", AT

        clock = [0.0]
        backend = _Backend([CAMERA])
        live = relay(backend, clock, {CAMERA: _Frozen()})
        for _ in range(20):
            clock[0] += 0.1
            live.step()
        self.assertEqual(len(backend.frames), 1)

    def test_live_view_can_be_switched_off_per_site(self) -> None:
        base = {"backend_url": "https://x", "site_key": "k", "cameras": {CAMERA: "0"}, "models": {"detector": "d.onnx"}}
        self.assertTrue(parse_local_config(base, environ={}).live_view)
        self.assertFalse(parse_local_config({**base, "live_view": False}, environ={}).live_view)


@unittest.skipUnless(importlib.util.find_spec("cv2") and importlib.util.find_spec("numpy"), "opencv not installed")
class EncodeTest(unittest.TestCase):
    def test_frames_are_small_jpegs(self) -> None:
        import numpy

        from syncam_ai.site_worker.live import encode_live_frame

        jpeg = encode_live_frame(numpy.zeros((1080, 1920, 3), dtype=numpy.uint8), AT)
        self.assertEqual(jpeg[:2], b"\xff\xd8")
        self.assertLess(len(jpeg), 512 * 1024)


if __name__ == "__main__":
    unittest.main()
