import importlib.util
import pathlib
import sys
import unittest
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.detector import ObjectDetection  # noqa: E402
from syncam_ai.site_worker.weapons import WEAPON_DEV_YOLOV8, WeaponProcessor, decode_yolov8, person_crops  # noqa: E402

TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
START = datetime(2026, 11, 16, 23, 0, tzinfo=timezone.utc)
PERSON = ObjectDetection("person", 0.9, 0.4, 0.2, 0.6, 0.9)
GUN = ObjectDetection("firearm", 0.8, 0.55, 0.45, 0.6, 0.5)


class WeaponConfirmationTest(unittest.TestCase):
    def processor(self):
        return WeaponProcessor(model=None, model_version=WEAPON_DEV_YOLOV8.model_version)

    def test_three_consecutive_frames_raise_one_review_alert(self) -> None:
        processor = self.processor()
        results = [
            processor.confirm((TENANT, SITE), CAMERA, [GUN], [PERSON], START + timedelta(milliseconds=250 * index))
            for index in range(3)
        ]
        self.assertEqual([len(result) for result in results], [0, 0, 1])
        alert = results[2][0]
        self.assertEqual(alert.event["event_type"], "weapon_review")
        self.assertEqual(alert.event["zone_id"], CAMERA)
        self.assertTrue(alert.event["requires_human_review"])
        self.assertIn(GUN, alert.detections)

    def test_a_gap_or_missing_frame_resets_confirmation(self) -> None:
        processor = self.processor()
        processor.confirm((TENANT, SITE), CAMERA, [GUN], [PERSON], START)
        processor.confirm((TENANT, SITE), CAMERA, [], [PERSON], START + timedelta(milliseconds=250))
        processor.confirm((TENANT, SITE), CAMERA, [GUN], [PERSON], START + timedelta(milliseconds=500))
        self.assertEqual(processor.confirm((TENANT, SITE), CAMERA, [GUN], [PERSON], START + timedelta(milliseconds=750)), [])

    def test_repeat_alerts_are_suppressed_for_a_minute(self) -> None:
        processor = self.processor()
        total = 0
        moment = START
        for _ in range(40):  # ten seconds of the same weapon at 4 fps
            moment += timedelta(milliseconds=250)
            total += len(processor.confirm((TENANT, SITE), CAMERA, [GUN], [PERSON], moment))
        self.assertEqual(total, 1)

    def test_no_people_means_no_weapon_inference(self) -> None:
        processor = self.processor()
        self.assertEqual(processor.process((TENANT, SITE), CAMERA, object(), [ObjectDetection("car", 0.9, 0, 0, 1, 1)], START), [])

    def test_person_crops_are_enlarged_and_bounded(self) -> None:
        crops = person_crops([PERSON, ObjectDetection("person", 0.9, 0.0, 0.0, 0.01, 0.01)], 1000, 500)
        self.assertEqual(len(crops), 1, "tiny people are not cropped")
        left, top, right, bottom = crops[0]
        self.assertTrue(0 <= left < 400 and 0 <= top and right > 600 and bottom <= 500)

    def test_dev_model_is_marked_development_only(self) -> None:
        self.assertTrue(WEAPON_DEV_YOLOV8.dev_only)


@unittest.skipUnless(importlib.util.find_spec("numpy"), "numpy not installed")
class DecodeYoloV8Test(unittest.TestCase):
    def test_maps_crop_boxes_back_to_the_frame(self) -> None:
        import numpy

        output = numpy.zeros((6, 4), dtype=numpy.float32)
        output[:, 1] = [320, 320, 64, 64, 0.1, 0.9]  # a knife in the middle of the crop
        output[:, 2] = [100, 100, 10, 10, 0.2, 0.1]  # below threshold
        found = decode_yolov8(output, ratio=2.0, offset=(0, 0), region=(100, 50, 420, 370), frame_width=1000, frame_height=500, minimum_confidence=0.5)
        self.assertEqual(len(found), 1)
        knife = found[0]
        self.assertEqual(knife.subject_class, "knife")
        self.assertAlmostEqual(knife.left, (100 + (320 - 32) / 2) / 1000)
        self.assertAlmostEqual(knife.top, (50 + (320 - 32) / 2) / 500)
        with self.assertRaises(ValueError):
            decode_yolov8(numpy.zeros((5, 4)), ratio=1, offset=(0, 0), region=(0, 0, 1, 1), frame_width=1, frame_height=1, minimum_confidence=0.5)


if __name__ == "__main__":
    unittest.main()
