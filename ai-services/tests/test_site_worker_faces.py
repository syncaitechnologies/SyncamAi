import importlib.util
import pathlib
import sys
import tempfile
import unittest
from datetime import datetime, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.client import BackendError  # noqa: E402
from syncam_ai.site_worker.faces import CONFIRMATIONS, AttendanceProcessor, best_match, cosine  # noqa: E402

ASHA = "11111111-1111-4111-8111-111111111111"
RAVI = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
AT = datetime(2026, 11, 13, 9, 0, tzinfo=timezone.utc)
GALLERY = {ASHA: [[1.0, 0.0, 0.0], [0.9, 0.1, 0.0]], RAVI: [[0.0, 1.0, 0.0]]}


class MatchTest(unittest.TestCase):
    def test_best_match_threshold_and_margin(self) -> None:
        self.assertAlmostEqual(cosine([1, 0], [1, 0]), 1.0)
        self.assertEqual(cosine([0, 0], [1, 0]), 0.0)
        match = best_match([0.95, 0.05, 0.0], GALLERY, threshold=0.6)
        self.assertEqual(match.staff_id, ASHA)
        self.assertIsNone(best_match([0.0, 0.0, 1.0], GALLERY, threshold=0.6), "strangers are ignored")
        # ~0.73 similar to Asha and ~0.76 to Ravi: too close to call.
        self.assertIsNone(best_match([0.65, 0.75, 0.0], GALLERY, threshold=0.6), "ambiguous look-alikes are ignored")
        self.assertIsNone(best_match([1.0, 0.0, 0.0], {}, threshold=0.6))


class _Gallery:
    def signatures(self):
        return GALLERY


class _Backend:
    def __init__(self, staff, fail=0):
        self.staff = staff
        self.fail = fail
        self.posted = []

    def config(self):
        return {"staff": [{"id": staff_id} for staff_id in self.staff]}

    def post_attendance(self, sighting):
        if self.fail:
            self.fail -= 1
            raise BackendError("offline")
        self.posted.append(sighting)


class AttendanceTest(unittest.TestCase):
    def processor(self, backend, clock):
        return AttendanceProcessor(engine=None, gallery=_Gallery(), backend=backend, entrance_cameras=frozenset({CAMERA}), threshold=0.6, clock=lambda: clock[0])

    def test_two_matches_mark_attendance_once_per_ten_minutes(self) -> None:
        clock = [0.0]
        backend = _Backend([ASHA, RAVI])
        processor = self.processor(backend, clock)
        processor._refresh()
        results = []
        for _ in range(CONFIRMATIONS + 3):
            clock[0] += 1
            results.append(processor.observe(CAMERA, [[1.0, 0.0, 0.0]], AT))
        self.assertEqual(sum(len(result) for result in results), 1)
        sighting = next(result for result in results if result)[0]
        self.assertEqual((sighting["staff_id"], sighting["camera_id"]), (ASHA, CAMERA))
        clock[0] += 601
        processor.observe(CAMERA, [[1.0, 0.0, 0.0]], AT)
        self.assertEqual(len(processor.observe(CAMERA, [[1.0, 0.0, 0.0]], AT)), 1, "a new visit after ten minutes is recorded")

    def test_withdrawn_staff_are_never_matched(self) -> None:
        clock = [0.0]
        processor = self.processor(_Backend([RAVI]), clock)
        processor._refresh()
        for _ in range(4):
            clock[0] += 1
            self.assertEqual(processor.observe(CAMERA, [[1.0, 0.0, 0.0]], AT), [])

    def test_failed_posts_are_retried(self) -> None:
        clock = [0.0]
        backend = _Backend([ASHA], fail=1)
        processor = self.processor(backend, clock)
        processor._pending = [{"id": "x"}]
        processor.flush()
        self.assertEqual((len(processor._pending), backend.posted), (1, []))
        processor.flush()
        self.assertEqual((len(processor._pending), backend.posted), (0, [{"id": "x"}]))

    def test_non_entrance_cameras_and_empty_scenes_are_skipped(self) -> None:
        processor = self.processor(_Backend([ASHA]), [0.0])
        self.assertEqual(processor.process(("t", "s"), "other-camera", None, [], AT), [])
        self.assertEqual(processor.process(("t", "s"), CAMERA, None, [], AT), [])


@unittest.skipUnless(importlib.util.find_spec("cryptography"), "cryptography not installed")
class GalleryTest(unittest.TestCase):
    def test_gallery_is_encrypted_and_supports_forgetting(self) -> None:
        from syncam_ai.site_worker.faces import FaceGallery

        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "faces.gallery"
            key = FaceGallery.new_key()
            gallery = FaceGallery(path, key)
            gallery.enroll(ASHA, "Asha", [[0.1, 0.2], [0.3, 0.4]])
            self.assertNotIn(b"Asha", path.read_bytes(), "names and signatures are not stored in clear text")
            self.assertEqual(FaceGallery(path, key).signatures(), {ASHA: [[0.1, 0.2], [0.3, 0.4]]})
            with self.assertRaises(Exception):
                FaceGallery(path, FaceGallery.new_key()).load()
            self.assertTrue(gallery.forget(ASHA))
            self.assertFalse(gallery.forget(ASHA))
            self.assertEqual(gallery.signatures(), {})


if __name__ == "__main__":
    unittest.main()
