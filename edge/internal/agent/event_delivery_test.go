package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const (
	reviewTenant = "11111111-1111-4111-8111-111111111111"
	reviewSite   = "22222222-2222-4222-8222-222222222222"
)

type recordingReviewSender struct {
	err      error
	payloads [][]byte
}

func (s *recordingReviewSender) SendReviewEvent(_ context.Context, payload []byte) error {
	if s.err != nil {
		return s.err
	}
	s.payloads = append(s.payloads, append([]byte(nil), payload...))
	return nil
}

func TestReviewEventDeliveryQueuesCanonicalEventsAndDeliversInOrder(t *testing.T) {
	spool, err := NewDurableSpool(t.TempDir(), 16*1024, 4096)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	current := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	spool.now = func() time.Time { current = current.Add(time.Second); return current }
	sender := &recordingReviewSender{}
	delivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, spool, sender)
	if err != nil {
		t.Fatalf("new delivery: %v", err)
	}

	firstPayload := reviewEventPayload("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "loitering")
	first, err := delivery.Queue(firstPayload)
	if err != nil {
		t.Fatalf("queue first event: %v", err)
	}
	replay, err := delivery.Queue(firstPayload)
	if err != nil || replay != first {
		t.Fatalf("idempotent replay: item=%+v err=%v", replay, err)
	}
	if _, err := delivery.Queue(reviewEventPayload("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "fire_review")); err != nil {
		t.Fatalf("queue second event: %v", err)
	}

	for _, expectedID := range []string{
		"review-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"review-bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
	} {
		item, err := delivery.DeliverNext(context.Background())
		if err != nil || item.ID != expectedID {
			t.Fatalf("deliver next: item=%+v expected=%s err=%v", item, expectedID, err)
		}
	}
	if _, err := delivery.DeliverNext(context.Background()); !errors.Is(err, ErrSpoolEmpty) {
		t.Fatalf("expected empty queue, got %v", err)
	}
	if len(sender.payloads) != 2 {
		t.Fatalf("unexpected send count: %d", len(sender.payloads))
	}
	for _, payload := range sender.payloads {
		var event map[string]any
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("decode delivered event: %v", err)
		}
		if event["requires_human_review"] != true || event["review_state"] != "pending" {
			t.Fatalf("unsafe review state: %s", payload)
		}
		if _, present := event["observed_behavior"]; present {
			t.Fatalf("non-vehicle metadata leaked into delivery: %s", payload)
		}
	}
}

func TestReviewEventDeliveryPreservesCanonicalVehicleScopeAndMetadata(t *testing.T) {
	spool, err := NewDurableSpool(t.TempDir(), 16*1024, 4096)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	sender := &recordingReviewSender{}
	delivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, spool, sender)
	if err != nil {
		t.Fatalf("new delivery: %v", err)
	}
	value := reviewEventMap("cccccccc-cccc-4ccc-8ccc-cccccccccccc", "vehicle_activity")
	value["observed_behavior"] = "detected"
	value["subject_class"] = "car"
	payload, _ := json.Marshal(value)
	if _, err := delivery.Queue(payload); err != nil {
		t.Fatalf("queue vehicle event: %v", err)
	}
	if _, err := delivery.DeliverNext(context.Background()); err != nil {
		t.Fatalf("deliver vehicle event: %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(sender.payloads[0], &event); err != nil {
		t.Fatalf("decode vehicle event: %v", err)
	}
	for key, expected := range map[string]any{
		"tenant_id":         reviewTenant,
		"site_id":           reviewSite,
		"camera_id":         "33333333-3333-4333-8333-333333333333",
		"zone_id":           "44444444-4444-4444-8444-444444444444",
		"observed_behavior": "detected",
		"subject_class":     "car",
	} {
		if event[key] != expected {
			t.Fatalf("%s scope or metadata changed: got=%v want=%v", key, event[key], expected)
		}
	}
}

func TestReviewEventDeliveryRetainsOnDownstreamFailureAndRecovers(t *testing.T) {
	root := t.TempDir()
	spool, err := NewDurableSpool(root, 16*1024, 4096)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	downstreamFailure := errors.New("synthetic downstream unavailable")
	sender := &recordingReviewSender{err: downstreamFailure}
	delivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, spool, sender)
	if err != nil {
		t.Fatalf("new delivery: %v", err)
	}
	if _, err := delivery.Queue(reviewEventPayload("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "smoke_review")); err != nil {
		t.Fatalf("queue event: %v", err)
	}
	if _, err := delivery.DeliverNext(context.Background()); !errors.Is(err, downstreamFailure) {
		t.Fatalf("expected downstream error, got %v", err)
	}
	if metrics := spool.Metrics(); metrics.Depth != 1 || metrics.AckedTotal != 0 {
		t.Fatalf("failed delivery changed the queue: %+v", metrics)
	}

	recovered, err := NewDurableSpool(root, 16*1024, 4096)
	if err != nil {
		t.Fatalf("recover spool: %v", err)
	}
	recoveredSender := &recordingReviewSender{}
	recoveredDelivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, recovered, recoveredSender)
	if err != nil {
		t.Fatalf("new recovered delivery: %v", err)
	}
	if _, err := recoveredDelivery.DeliverNext(context.Background()); err != nil {
		t.Fatalf("deliver recovered event: %v", err)
	}
	if metrics := recovered.Metrics(); metrics.Depth != 0 || metrics.AckedTotal != 1 {
		t.Fatalf("unexpected recovered metrics: %+v", metrics)
	}
}

func TestReviewEventDeliveryBackpressuresWithoutEviction(t *testing.T) {
	probe, err := NewDurableSpool(t.TempDir(), 16*1024, 4096)
	if err != nil {
		t.Fatalf("new probe spool: %v", err)
	}
	probeDelivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, probe, &recordingReviewSender{})
	if err != nil {
		t.Fatalf("new probe delivery: %v", err)
	}
	if _, err := probeDelivery.Queue(reviewEventPayload("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "weapon_review")); err != nil {
		t.Fatalf("queue probe: %v", err)
	}
	firstRecordBytes := probe.Metrics().Bytes

	spool, err := NewDurableSpool(t.TempDir(), firstRecordBytes+32, firstRecordBytes)
	if err != nil {
		t.Fatalf("new bounded spool: %v", err)
	}
	delivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, spool, &recordingReviewSender{})
	if err != nil {
		t.Fatalf("new delivery: %v", err)
	}
	firstPayload := reviewEventPayload("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "weapon_review")
	if _, err := delivery.Queue(firstPayload); err != nil {
		t.Fatalf("queue first event: %v", err)
	}
	before := spool.Metrics()
	if _, err := delivery.Queue(reviewEventPayload("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "weapon_review")); !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("expected capacity backpressure, got %v", err)
	}
	after := spool.Metrics()
	if after.Depth != before.Depth || after.Bytes != before.Bytes ||
		after.EnqueuedTotal != before.EnqueuedTotal || after.AckedTotal != before.AckedTotal ||
		after.EvictedTotal != before.EvictedTotal || after.EvictedTotal != 0 {
		t.Fatalf("capacity failure was not atomic: before=%+v after=%+v", before, after)
	}
	message, err := spool.NextPriority(SpoolMetadata)
	if err != nil || message.ID != "review-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("old event was displaced: message=%+v err=%v", message, err)
	}
}

func TestReviewEventDeliveryRejectsMalformedUnsafeReplayAndWrongScope(t *testing.T) {
	spool, err := NewDurableSpool(t.TempDir(), 32*1024, 4096)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	delivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, spool, &recordingReviewSender{})
	if err != nil {
		t.Fatalf("new delivery: %v", err)
	}
	base := reviewEventMap("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "ppe_review")
	cases := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr error
	}{
		{name: "missing review state", mutate: func(value map[string]any) { delete(value, "review_state") }, wantErr: ErrInvalidReviewEvent},
		{name: "unknown autonomous action", mutate: func(value map[string]any) { value["dispatch"] = "emergency" }, wantErr: ErrInvalidReviewEvent},
		{name: "not pending", mutate: func(value map[string]any) { value["review_state"] = "confirmed" }, wantErr: ErrInvalidReviewEvent},
		{name: "human review disabled", mutate: func(value map[string]any) { value["requires_human_review"] = false }, wantErr: ErrInvalidReviewEvent},
		{name: "biometric type", mutate: func(value map[string]any) { value["event_type"] = "attendance_review" }, wantErr: ErrInvalidReviewEvent},
		{name: "track-bearing dedupe", mutate: func(value map[string]any) { value["dedupe_key"] = "ppe_review:track-42" }, wantErr: ErrInvalidReviewEvent},
		{name: "wrong tenant", mutate: func(value map[string]any) { value["tenant_id"] = "99999999-9999-4999-8999-999999999999" }, wantErr: ErrReviewEventScope},
		{name: "vehicle metadata on PPE", mutate: func(value map[string]any) { value["subject_class"] = "person" }, wantErr: ErrInvalidReviewEvent},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := cloneReviewEventMap(base)
			test.mutate(value)
			payload, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				t.Fatalf("marshal case: %v", marshalErr)
			}
			if _, err := delivery.Queue(payload); !errors.Is(err, test.wantErr) {
				t.Fatalf("expected %v, got %v", test.wantErr, err)
			}
		})
	}
	if metrics := spool.Metrics(); metrics.Depth != 0 || metrics.EnqueuedTotal != 0 {
		t.Fatalf("invalid events mutated the queue: %+v", metrics)
	}

	valid := reviewEventPayload("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "ppe_review")
	if _, err := delivery.Queue(valid); err != nil {
		t.Fatalf("queue valid event: %v", err)
	}
	changed := reviewEventMap("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "ppe_review")
	changed["confidence"] = 0.5
	changedPayload, _ := json.Marshal(changed)
	if _, err := delivery.Queue(changedPayload); !errors.Is(err, ErrSpoolConflict) {
		t.Fatalf("expected replay conflict, got %v", err)
	}
}

func TestReviewEventDeliveryRequiresValidCompositionAndSkipsOtherPriorities(t *testing.T) {
	spool, err := NewDurableSpool(t.TempDir(), 16*1024, 4096)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	if _, err := NewReviewEventDelivery("bad", reviewSite, spool, &recordingReviewSender{}); !errors.Is(err, ErrReviewDeliveryConfig) {
		t.Fatalf("expected invalid delivery scope, got %v", err)
	}
	if _, err := NewReviewEventDelivery(reviewTenant, reviewSite, nil, &recordingReviewSender{}); !errors.Is(err, ErrReviewDeliveryConfig) {
		t.Fatalf("expected missing spool error, got %v", err)
	}
	if _, err := spool.Enqueue("evidence-1", SpoolEvidence, []byte("synthetic evidence metadata")); err != nil {
		t.Fatalf("queue other priority: %v", err)
	}
	delivery, err := NewReviewEventDelivery(reviewTenant, reviewSite, spool, &recordingReviewSender{})
	if err != nil {
		t.Fatalf("new delivery: %v", err)
	}
	if _, err := delivery.DeliverNext(context.Background()); !errors.Is(err, ErrSpoolEmpty) {
		t.Fatalf("expected no review metadata, got %v", err)
	}
	if metrics := spool.Metrics(); metrics.Depth != 1 {
		t.Fatalf("delivery consumed another priority: %+v", metrics)
	}
}

func reviewEventPayload(eventID, eventType string) []byte {
	payload, err := json.Marshal(reviewEventMap(eventID, eventType))
	if err != nil {
		panic(err)
	}
	return payload
}

func reviewEventMap(eventID, eventType string) map[string]any {
	return map[string]any{
		"event_id":              eventID,
		"tenant_id":             reviewTenant,
		"dedupe_key":            eventType + ":" + eventID,
		"occurred_at":           "2026-09-14T06:00:00Z",
		"site_id":               reviewSite,
		"camera_id":             "33333333-3333-4333-8333-333333333333",
		"zone_id":               "44444444-4444-4444-8444-444444444444",
		"event_type":            eventType,
		"model_version":         "synthetic-model-metadata-1",
		"confidence":            0.75,
		"evidence_refs":         []string{},
		"requires_human_review": true,
		"review_state":          "pending",
	}
}

func cloneReviewEventMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
