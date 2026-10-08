"""Tests for the non-persistent local person-inference boundary."""

from __future__ import annotations

from pathlib import Path
import sys
import unittest

SRC = Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.local_person_inference import (
    LocalFrame,
    decode_local_frame,
    local_person_response,
    validate_local_inference_origin,
)


class LocalPersonInferenceRequestTest(unittest.TestCase):
    def test_decodes_only_exactly_bounded_rgba_frames(self) -> None:
        frame = decode_local_frame(
            "/local-dev/v1/person-detections?width=2&height=1",
            "application/octet-stream",
            b"\x00\x01\x02\x03\x04\x05\x06\x07",
        )

        self.assertEqual(frame, LocalFrame(2, 1, b"\x00\x01\x02\x03\x04\x05\x06\x07"))

    def test_rejects_wrong_route_media_type_dimensions_and_payload_length(self) -> None:
        cases = [
            ("/v1/person-detections?width=2&height=1", "application/octet-stream", b"\x00" * 8),
            ("/local-dev/v1/person-detections?width=2&height=1", "image/jpeg", b"\x00" * 8),
            ("/local-dev/v1/person-detections?width=641&height=1", "application/octet-stream", b"\x00"),
            ("/local-dev/v1/person-detections?width=2&height=1", "application/octet-stream", b"\x00" * 7),
        ]
        for path, content_type, body in cases:
            with self.subTest(path=path, content_type=content_type, size=len(body)):
                with self.assertRaises(ValueError):
                    decode_local_frame(path, content_type, body)


class LocalPersonInferenceOriginTest(unittest.TestCase):
    def test_allows_only_one_clean_https_origin(self) -> None:
        self.assertEqual(
            validate_local_inference_origin("https://demo.syncam.example/"),
            "https://demo.syncam.example",
        )
        for origin in ("*", "http://demo.syncam.example", "https://user@demo.syncam.example", "https://demo.syncam.example/path"):
            with self.subTest(origin=origin):
                with self.assertRaises(ValueError):
                    validate_local_inference_origin(origin)


class LocalPersonInferenceResponseTest(unittest.TestCase):
    def test_response_exposes_only_person_box_fields(self) -> None:
        from syncam_ai.local_person_inference import LocalPersonBox

        response = local_person_response((LocalPersonBox(0.9, 1, 2, 3, 4),))
        self.assertEqual(response, {"detections": [{"confidence": 0.9, "left": 1, "top": 2, "right": 3, "bottom": 4}]})
