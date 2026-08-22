import json
import pathlib
import sys
import unittest
from dataclasses import replace
from datetime import datetime, timedelta, timezone

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.zone_rules import (  # noqa: E402
    TrackObservation,
    ZoneRule,
    ZoneRuleEngine,
    load_zone_rules,
)


BASE_TIME = datetime(2026, 8, 21, 7, 0, tzinfo=timezone.utc)
TENANT = "11111111-1111-4111-8111-111111111111"
SITE = "22222222-2222-4222-8222-222222222222"
CAMERA = "33333333-3333-4333-8333-333333333333"
ZONE = "44444444-4444-4444-8444-444444444444"


def polygon() -> dict[str, object]:
    return {
        "type": "Polygon",
        "coordinates": [[[0, 0], [10, 0], [10, 10], [0, 10], [0, 0]]],
    }


def rule(kind: str = "intrusion", **changes: object) -> ZoneRule:
    values: dict[str, object] = {
        "id": ZONE,
        "tenant_id": TENANT,
        "site_id": SITE,
        "camera_id": CAMERA,
        "kind": kind,
        "geometry": polygon() if kind != "tripwire" else {"type": "LineString", "coordinates": [[5, 0], [5, 10]]},
        "loiter_seconds": 30,
    }
    values.update(changes)
    return ZoneRule(**values)  # type: ignore[arg-type]


def observation(seconds: int, x: float, y: float, **changes: object) -> TrackObservation:
    values: dict[str, object] = {
        "tenant_id": TENANT,
        "site_id": SITE,
        "camera_id": CAMERA,
        "track_id": 42,
        "observed_at": BASE_TIME + timedelta(seconds=seconds),
        "center_x": x,
        "center_y": y,
        "subject_class": " person ",
        "confidence": 0.91,
        "model_version": " tracker-logic-1 ",
        "evidence_refs": (" evidence://track-42 ",),
    }
    values.update(changes)
    return TrackObservation(**values)  # type: ignore[arg-type]


class ZoneRuleEngineTest(unittest.TestCase):
    def test_intrusion_and_restricted_zone_emit_only_on_entry(self) -> None:
        for kind in ("intrusion", "restricted_zone"):
            with self.subTest(kind=kind):
                engine = ZoneRuleEngine([rule(kind)])
                self.assertEqual(engine.observe(observation(0, -1, 5)), [])
                events = engine.observe(observation(1, 5, 5))
                self.assertEqual(len(events), 1)
                self.assertEqual(events[0]["event_type"], kind)
                self.assertEqual(events[0]["observed_behavior"], "entered")
                self.assertEqual(engine.observe(observation(2, 6, 5)), [])

    def test_subject_class_gate_ignores_unconfigured_local_tracks(self) -> None:
        engine = ZoneRuleEngine([rule("intrusion", subject_classes=frozenset({"person"}))])
        self.assertEqual(engine.observe(observation(0, -1, 5, subject_class="car")), [])
        self.assertEqual(engine.observe(observation(1, 5, 5, subject_class="car")), [])
        self.assertEqual(engine.observe(observation(2, -1, 5, subject_class="person")), [])
        events = engine.observe(observation(3, 5, 5, subject_class="person"))
        self.assertEqual(len(events), 1)

    def test_track_state_is_bounded_and_stale_entries_are_evicted(self) -> None:
        atomic_engine = ZoneRuleEngine(
            [rule(), replace(rule(), id="55555555-5555-4555-8555-555555555555")],
            max_track_states=1,
        )
        with self.assertRaisesRegex(ValueError, "capacity"):
            atomic_engine.observe(observation(0, -1, 5, track_id=1))
        self.assertEqual(atomic_engine.track_state_count, 0)

        engine = ZoneRuleEngine([rule()], max_track_states=1, state_ttl_seconds=600)
        self.assertEqual(engine.observe(observation(0, -1, 5, track_id=1)), [])
        self.assertEqual(engine.track_state_count, 1)
        with self.assertRaisesRegex(ValueError, "capacity"):
            engine.observe(observation(1, -1, 5, track_id=2))
        self.assertEqual(engine.track_state_count, 1)
        self.assertEqual(engine.observe(observation(601, -1, 5, track_id=2)), [])
        self.assertEqual(engine.track_state_count, 1)

    def test_state_bounds_preserve_maximum_loiter_duration(self) -> None:
        engine = ZoneRuleEngine([rule("loitering", loiter_seconds=600)], state_ttl_seconds=600)
        self.assertEqual(engine.observe(observation(0, -1, 5)), [])
        self.assertEqual(engine.observe(observation(1, 5, 5)), [])
        self.assertEqual(len(engine.observe(observation(601, 5, 5))), 1)

        for kwargs in (
            {"max_track_states": 0},
            {"state_ttl_seconds": 599},
            {"state_ttl_seconds": 901},
        ):
            with self.subTest(kwargs=kwargs):
                with self.assertRaises(ValueError):
                    ZoneRuleEngine([rule()], **kwargs)

    def test_loitering_uses_dwell_and_emits_once(self) -> None:
        engine = ZoneRuleEngine([rule("loitering")])
        self.assertEqual(engine.observe(observation(0, -1, 5)), [])
        self.assertEqual(engine.observe(observation(1, 5, 5)), [])
        self.assertEqual(engine.observe(observation(30, 5, 5)), [])
        events = engine.observe(observation(31, 5, 5))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "loitering")
        self.assertEqual(events[0]["observed_behavior"], "dwell_exceeded")
        self.assertEqual(engine.observe(observation(62, 5, 5)), [])
        self.assertEqual(engine.observe(observation(63, -1, 5)), [])

    def test_tripwire_requires_a_real_side_change(self) -> None:
        engine = ZoneRuleEngine([rule("tripwire")])
        self.assertEqual(engine.observe(observation(0, 3, 5)), [])
        events = engine.observe(observation(1, 7, 5))
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["event_type"], "tripwire")
        self.assertEqual(events[0]["observed_behavior"], "crossed")
        self.assertEqual(engine.observe(observation(2, 8, 5)), [])

    def test_events_are_retry_stable_and_do_not_leak_track_geometry_or_identity(self) -> None:
        first = ZoneRuleEngine([rule()])
        second = ZoneRuleEngine([rule()])
        for engine in (first, second):
            engine.observe(observation(0, -1, 5))
        event = first.observe(observation(1, 5, 5))[0]
        self.assertEqual(event, second.observe(observation(1, 5, 5))[0])
        serialized = json.dumps(event, sort_keys=True).lower()
        for prohibited in ("track_id", "center_x", "center_y", "identity", "embedding", "plate", "reid", "pixel"):
            self.assertNotIn(prohibited, serialized)

    def test_scope_timestamp_and_input_bounds_fail_closed(self) -> None:
        engine = ZoneRuleEngine([rule()])
        self.assertEqual(engine.observe(observation(0, -1, 5, camera_id="55555555-5555-4555-8555-555555555555")), [])
        engine.observe(observation(1, -1, 5))
        with self.assertRaises(ValueError):
            engine.observe(observation(0, 5, 5))
        for invalid in (
            replace(observation(2, 5, 5), track_id=True),
            replace(observation(2, 5, 5), center_x=float("nan")),
            replace(observation(2, 5, 5), confidence=1.1),
            replace(observation(2, 5, 5), subject_class=" "),
            replace(observation(2, 5, 5), evidence_refs=(" ",)),
        ):
            with self.subTest(value=invalid):
                with self.assertRaises(ValueError):
                    engine.observe(invalid)

    def test_loader_rejects_masks_and_skips_non_camera_or_other_runtime_kinds(self) -> None:
        payload = {"zones": [
            {"id": ZONE, "tenant_id": TENANT, "site_id": SITE, "camera_id": CAMERA, "kind": "loitering", "loiter_seconds": 90, "subject_classes": ["person"], "enabled": True, "geometry": polygon()},
            {"id": "55555555-5555-4555-8555-555555555555", "tenant_id": TENANT, "site_id": SITE, "camera_id": "", "kind": "intrusion", "enabled": True, "geometry": polygon()},
            {"id": "66666666-6666-4666-8666-666666666666", "tenant_id": TENANT, "site_id": SITE, "camera_id": CAMERA, "kind": "abandoned", "enabled": True, "geometry": polygon()},
        ]}
        loaded = load_zone_rules(payload)
        self.assertEqual(len(loaded), 1)
        self.assertEqual(loaded[0].loiter_seconds, 90)
        self.assertEqual(loaded[0].subject_classes, frozenset({"person"}))
        payload["zones"].append({"id": "77777777-7777-4777-8777-777777777777", "tenant_id": TENANT, "site_id": SITE, "camera_id": CAMERA, "kind": "mask", "enabled": True, "geometry": polygon()})
        with self.assertRaises(ValueError):
            load_zone_rules(payload)
        invalid_classes = {"zones": [{"id": ZONE, "tenant_id": TENANT, "site_id": SITE, "camera_id": CAMERA, "kind": "intrusion", "subject_classes": ["animal"], "enabled": True, "geometry": polygon()}]}
        with self.assertRaises(ValueError):
            load_zone_rules(invalid_classes)

    def test_rule_geometry_and_dwell_bounds_fail_closed(self) -> None:
        invalid = (
            rule("intrusion", geometry={"type": "LineString", "coordinates": [[0, 0], [1, 1]]}),
            rule("intrusion", geometry={"type": "Polygon", "coordinates": [[[0, 0], [1, 1], [2, 2], [0, 0]]]}),
            rule("loitering", loiter_seconds=0),
            rule("mask"),
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    ZoneRuleEngine([value])
        payload = {"zones": [{"id": ZONE, "tenant_id": TENANT, "site_id": SITE, "camera_id": CAMERA, "kind": "loitering", "loiter_seconds": 29, "enabled": True, "geometry": polygon()}]}
        with self.assertRaises(ValueError):
            load_zone_rules(payload)


if __name__ == "__main__":
    unittest.main()
