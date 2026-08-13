import assert from "node:assert/strict";
import test from "node:test";

import {
  alertFromEvent,
  alertsFromSnapshot,
  initialAlertQueue,
  parseAlertListEnvelope,
  parseRealtimeEnvelope,
  parseTicketEnvelope,
  sortAlerts,
  toAlertItem,
  upsertAlert,
  type ApiAlert,
} from "./alert-contracts.ts";

const alert: ApiAlert = {
  id: "11111111-1111-4111-8111-111111111111",
  tenant_id: "22222222-2222-4222-8222-222222222222",
  event_id: "33333333-3333-4333-8333-333333333333",
  site_id: "44444444-4444-4444-8444-444444444444",
  camera_id: "55555555-5555-4555-8555-555555555555",
  zone_id: "66666666-6666-4666-8666-666666666666",
  event_type: "intrusion",
  severity: "high",
  status: "unacknowledged",
  confidence: 0.91,
  occurred_at: "2026-08-13T18:31:00Z",
  created_at: "2026-08-13T18:31:01Z",
};

test("parses the canonical alert list and maps it to the operator model", () => {
  const parsed = parseAlertListEnvelope({
    data: [alert],
    meta: { count: 1, next: null },
  });
  assert.equal(parsed[0]?.id, alert.id);
  const mapped = toAlertItem(parsed[0]!);
  assert.equal(mapped.title, "Restricted zone entry");
  assert.equal(mapped.severity, "High");
  assert.equal(mapped.siteId, alert.site_id);
});

test("rejects malformed REST and ticket envelopes", () => {
  assert.throws(
    () =>
      parseAlertListEnvelope({ data: [{ ...alert, confidence: 2 }], meta: {} }),
    /v1 contract/,
  );
  assert.throws(
    () =>
      parseTicketEnvelope({
        data: {
          ticket: "short",
          expires_at: "2026-08-13T18:31:00Z",
          protocol: "syncam.realtime.v1",
        },
      }),
    /ticket response/,
  );
});

test("parses snapshots and state events while rejecting unsupported topics", () => {
  const snapshot = parseRealtimeEnvelope({
    v: 1,
    type: "snapshot",
    topic: "alerts.*",
    seq: 7,
    ts: "2026-08-13T18:31:00Z",
    payload: { alerts: [alert], base_seq: 7 },
  });
  assert.equal(alertsFromSnapshot(snapshot)[0]?.id, alert.id);

  const state = parseRealtimeEnvelope({
    v: 1,
    type: "event",
    topic: "alerts.state",
    seq: 8,
    ts: "2026-08-13T18:31:01Z",
    payload: {
      alert: { ...alert, status: "acknowledged" },
      action: "acknowledge",
    },
  });
  assert.equal(alertFromEvent(state)?.status, "acknowledged");
  assert.throws(
    () =>
      parseRealtimeEnvelope({
        v: 1,
        type: "event",
        topic: "tenant.leak",
        seq: 9,
        ts: "2026-08-13T18:31:02Z",
      }),
    /unsupported topic/,
  );
});

test("upserts realtime state and retains queue priority ordering", () => {
  const existing = toAlertItem(alert);
  const critical = toAlertItem({
    ...alert,
    id: "77777777-7777-4777-8777-777777777777",
    event_id: "88888888-8888-4888-8888-888888888888",
    severity: "critical",
    occurred_at: "2026-08-13T18:32:00Z",
  });
  const sorted = sortAlerts([existing, critical]);
  assert.equal(sorted[0]?.severity, "Critical");

  const updated = upsertAlert(sorted, { ...alert, status: "acknowledged" });
  assert.equal(
    updated.find((item) => item.id === alert.id)?.status,
    "acknowledged",
  );
  assert.equal(updated.filter((item) => item.id === alert.id).length, 1);
});

test("keeps synthetic seed alerts out of live mode", () => {
  const demoAlert = toAlertItem(alert);
  assert.deepEqual(initialAlertQueue("live", [demoAlert]), []);
  assert.equal(initialAlertQueue("demo", [demoAlert])[0]?.id, demoAlert.id);
});
