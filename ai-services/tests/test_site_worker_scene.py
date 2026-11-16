"""Find cameras: what the box sees in each channel's picture (for the protection suggestion)."""

import importlib.util
import pathlib
import sys
import tempfile
import threading
import unittest
from types import SimpleNamespace

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.setup_agent import scene_counter  # noqa: E402

HAS_CV2 = importlib.util.find_spec("cv2") is not None and importlib.util.find_spec("numpy") is not None


class _Detector:
    def __init__(self, labels):
        self.labels = labels
        self.calls = []

    def detect(self, frame, *, minimum_confidence=0.45):
        self.calls.append(minimum_confidence)
        return [SimpleNamespace(subject_class=label) for label in self.labels]


class SceneCounterTest(unittest.TestCase):
    def test_counts_parked_vehicles_and_people(self) -> None:
        detector = _Detector(["car", "truck", "person", "bicycle", "van", "motorcycle", "person"])
        self.assertEqual(scene_counter(detector)(object()), {"vehicles": 3, "people": 2})
        self.assertEqual(detector.calls, [0.4])


@unittest.skipUnless(HAS_CV2, "OpenCV is needed to read a video file")
class ThumbnailInspectTest(unittest.TestCase):
    def setUp(self) -> None:
        import cv2
        import numpy

        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.video = str(pathlib.Path(self.directory.name) / "clip.avi")
        writer = cv2.VideoWriter(self.video, cv2.VideoWriter_fourcc(*"MJPG"), 5, (640, 360))
        for _ in range(5):
            writer.write(numpy.full((360, 640, 3), 90, dtype=numpy.uint8))
        writer.release()

    def test_inspect_sees_the_full_frame_and_adds_fields(self) -> None:
        from syncam_ai.site_worker.discovery import grab_thumbnail

        seen = []
        shot = grab_thumbnail(self.video, inspect=lambda frame: seen.append(frame.shape) or {"vehicles": 2, "people": 0})
        self.assertIsNotNone(shot)
        self.assertEqual(seen, [(360, 640, 3)])
        self.assertEqual((shot["vehicles"], shot["people"], shot["width"]), (2, 0, 640))
        self.assertTrue(shot["thumbnail"])

    def test_a_failing_inspect_keeps_the_picture(self) -> None:
        from syncam_ai.site_worker.discovery import grab_thumbnail

        def broken(frame):
            raise RuntimeError("model unavailable")

        shot = grab_thumbnail(self.video, inspect=broken)
        self.assertIsNotNone(shot)
        self.assertNotIn("vehicles", shot)


@unittest.skipUnless(HAS_CV2, "OpenCV and numpy are needed to prepare frames")
class DetectorLockTest(unittest.TestCase):
    def test_detect_serialises_inference(self) -> None:
        import numpy

        from syncam_ai.site_worker.detector import YoloxDetector

        active, peak = [0], [0]
        guard = threading.Lock()

        def model(tensor):
            with guard:
                active[0] += 1
                peak[0] = max(peak[0], active[0])
            threading.Event().wait(0.02)
            with guard:
                active[0] -= 1
            return {"out": numpy.zeros((1, 8400, 85), dtype=numpy.float32)}

        detector = YoloxDetector.__new__(YoloxDetector)
        detector._model = model
        detector._lock = threading.Lock()
        detector._input_size = 640
        from syncam_ai.site_worker.detector import COCO_SUBJECTS

        detector._class_map = COCO_SUBJECTS
        frame = numpy.zeros((360, 640, 3), dtype=numpy.uint8)
        errors = []

        def run():
            try:
                detector.detect(frame)
            except Exception as error:  # surfaced below; a thread would hide it
                errors.append(error)

        threads = [threading.Thread(target=run) for _ in range(4)]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()
        self.assertEqual(errors, [])
        self.assertEqual(peak[0], 1, "the live worker and Find cameras never infer at the same time")


if __name__ == "__main__":
    unittest.main()
