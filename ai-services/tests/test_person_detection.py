"""Tests for the local-only Apache-compatible person detector adapter."""

from __future__ import annotations

from hashlib import sha384
from pathlib import Path
import sys
import unittest

SRC = Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.person_detection import (
    PersonDetectorArtifact,
    parse_person_detections,
    verify_person_detector_artifact,
)


class PersonDetectorArtifactTest(unittest.TestCase):
    def test_accepts_exact_external_artifact_pair(self) -> None:
        xml_path, bin_path = _fixture_paths()
        artifact = PersonDetectorArtifact(
            "test",
            "Apache-2.0",
            "https://example.invalid/manifest",
            _file_digest(xml_path),
            _file_digest(bin_path),
        )

        verify_person_detector_artifact(xml_path, bin_path, artifact)

    def test_rejects_mismatched_or_non_apache_artifacts(self) -> None:
        xml_path, bin_path = _fixture_paths()
        artifact = PersonDetectorArtifact(
            "test",
            "MIT",
            "https://example.invalid/manifest",
            _file_digest(xml_path),
            _file_digest(bin_path),
        )

        with self.assertRaisesRegex(ValueError, "Apache-2.0"):
            verify_person_detector_artifact(xml_path, bin_path, artifact)


class PersonDetectionParserTest(unittest.TestCase):
    def test_parses_and_scales_documented_person_output(self) -> None:
        output = [[[[0, 0, 0.95, 0.1, 0.2, 0.6, 0.8], [-1, 0, 0, 0, 0, 0, 0]]]]

        detections = parse_person_detections(
            output, frame_width=1000, frame_height=500, minimum_confidence=0.5
        )

        self.assertEqual(detections[0].left, 100)
        self.assertEqual(detections[0].top, 100)
        self.assertEqual(detections[0].right, 600)
        self.assertEqual(detections[0].bottom, 400)

    def test_rejects_unexpected_output_contract(self) -> None:
        with self.assertRaisesRegex(ValueError, "1x1xNx7"):
            parse_person_detections(
                [[0, 0, 0, 0, 0, 0, 0]],
                frame_width=100,
                frame_height=100,
                minimum_confidence=0.5,
            )

    def test_rejects_unexpected_label_and_invalid_threshold(self) -> None:
        output = [[[[0, 1, 0.95, 0.1, 0.2, 0.6, 0.8]]]]
        with self.assertRaisesRegex(ValueError, "unexpected label"):
            parse_person_detections(
                output, frame_width=100, frame_height=100, minimum_confidence=0.5
            )
        with self.assertRaisesRegex(ValueError, "within"):
            parse_person_detections(
                output, frame_width=100, frame_height=100, minimum_confidence=1.1
            )


def _fixture_paths() -> tuple[Path, Path]:
    return Path(__file__), SRC / "syncam_ai" / "person_detection.py"


def _file_digest(path: Path) -> str:
    return sha384(path.read_bytes()).hexdigest()

