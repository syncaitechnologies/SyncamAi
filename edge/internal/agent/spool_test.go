package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableSpoolValidatesConfigurationAndItems(t *testing.T) {
	if _, err := NewDurableSpool("", 1024, 128); !errors.Is(err, ErrInvalidSpoolConfig) {
		t.Fatalf("expected invalid root, got %v", err)
	}
	if _, err := NewDurableSpool(t.TempDir(), 100, 101); !errors.Is(err, ErrInvalidSpoolConfig) {
		t.Fatalf("expected invalid quota, got %v", err)
	}
	spool, err := NewDurableSpool(t.TempDir(), 4096, 256)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	for _, test := range []struct {
		id       string
		priority SpoolPriority
		payload  []byte
	}{
		{id: "../escape", priority: SpoolMetadata, payload: []byte("event")},
		{id: "event-1", priority: "urgent", payload: []byte("event")},
		{id: "event-1", priority: SpoolMetadata},
		{id: "event-1", priority: SpoolMetadata, payload: bytes.Repeat([]byte("x"), 257)},
	} {
		if _, err := spool.Enqueue(test.id, test.priority, test.payload); !errors.Is(err, ErrInvalidSpoolItem) {
			t.Fatalf("item %+v: expected validation error, got %v", test, err)
		}
	}
}

func TestDurableSpoolIsIdempotentAndRequiresAcknowledgement(t *testing.T) {
	spool, err := NewDurableSpool(t.TempDir(), 4096, 256)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	spool.now = func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) }
	first, err := spool.Enqueue("event-1", SpoolMetadata, []byte(`{"kind":"alert"}`))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	replay, err := spool.Enqueue("event-1", SpoolMetadata, []byte(`{"kind":"alert"}`))
	if err != nil || replay != first {
		t.Fatalf("idempotent enqueue: item=%+v err=%v", replay, err)
	}
	if _, err := spool.Enqueue("event-1", SpoolMetadata, []byte(`{"kind":"changed"}`)); !errors.Is(err, ErrSpoolConflict) {
		t.Fatalf("expected conflicting replay, got %v", err)
	}
	message, err := spool.Next()
	if err != nil || message.ID != "event-1" || string(message.Payload) != `{"kind":"alert"}` {
		t.Fatalf("unexpected next message: %+v err=%v", message, err)
	}
	if err := spool.Ack(" event-1 "); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if _, err := spool.Next(); !errors.Is(err, ErrSpoolEmpty) {
		t.Fatalf("expected empty spool, got %v", err)
	}
	if err := spool.Ack("event-1"); !errors.Is(err, ErrSpoolItemNotFound) {
		t.Fatalf("expected missing acknowledgement, got %v", err)
	}
	metrics := spool.Metrics()
	if metrics.Depth != 0 || metrics.Bytes != 0 || metrics.EnqueuedTotal != 1 || metrics.AckedTotal != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestDurableSpoolRecoversAndReplaysByPriority(t *testing.T) {
	root := t.TempDir()
	spool, err := NewDurableSpool(root, 8192, 512)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	current := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	spool.now = func() time.Time { current = current.Add(time.Second); return current }
	if _, err := spool.Enqueue("video-1", SpoolVideo, []byte("video")); err != nil {
		t.Fatalf("enqueue video: %v", err)
	}
	if _, err := spool.Enqueue("evidence-1", SpoolEvidence, []byte("evidence")); err != nil {
		t.Fatalf("enqueue evidence: %v", err)
	}
	if _, err := spool.Enqueue("metadata-1", SpoolMetadata, []byte("metadata")); err != nil {
		t.Fatalf("enqueue metadata: %v", err)
	}

	recovered, err := NewDurableSpool(root, 8192, 512)
	if err != nil {
		t.Fatalf("recover spool: %v", err)
	}
	for _, want := range []string{"metadata-1", "evidence-1", "video-1"} {
		message, err := recovered.Next()
		if err != nil || message.ID != want {
			t.Fatalf("next: got=%+v want=%s err=%v", message, want, err)
		}
		if err := recovered.Ack(want); err != nil {
			t.Fatalf("ack %s: %v", want, err)
		}
	}
}

func TestDurableSpoolEvictsOldestToEnforceQuota(t *testing.T) {
	root := t.TempDir()
	spool, err := NewDurableSpool(root, 8192, 256)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	current := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	spool.now = func() time.Time { current = current.Add(time.Second); return current }
	for _, id := range []string{"oldest", "middle", "newest"} {
		if _, err := spool.Enqueue(id, SpoolMetadata, bytes.Repeat([]byte(id), 20)); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}
	before := spool.Metrics()
	limited, err := NewDurableSpool(root, before.Bytes-1, 256)
	if err != nil {
		t.Fatalf("reopen with quota: %v", err)
	}
	metrics := limited.Metrics()
	if metrics.Depth != 2 || metrics.Bytes > before.Bytes-1 || metrics.EvictedTotal != 1 {
		t.Fatalf("unexpected eviction metrics: before=%+v after=%+v", before, metrics)
	}
	if err := limited.Ack("oldest"); !errors.Is(err, ErrSpoolItemNotFound) {
		t.Fatalf("oldest item was not evicted: %v", err)
	}
	message, err := limited.Next()
	if err != nil || message.ID != "middle" {
		t.Fatalf("unexpected oldest survivor: %+v err=%v", message, err)
	}
}

func TestDurableSpoolFailsClosedOnCorruption(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "00000000000000000001-corrupt.msg"), []byte("not-json\npayload"), 0o600); err != nil {
		t.Fatalf("write corrupt item: %v", err)
	}
	if _, err := NewDurableSpool(root, 4096, 256); !errors.Is(err, ErrCorruptSpool) {
		t.Fatalf("expected corrupt spool error, got %v", err)
	}
}

func TestDurableSpoolRemovesIncompleteAtomicWriteOnRecovery(t *testing.T) {
	root := t.TempDir()
	temporaryPath := filepath.Join(root, ".spool-interrupted")
	if err := os.WriteFile(temporaryPath, []byte("incomplete"), 0o600); err != nil {
		t.Fatalf("write incomplete item: %v", err)
	}
	if _, err := NewDurableSpool(root, 4096, 256); err != nil {
		t.Fatalf("recover spool: %v", err)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete atomic write was not removed: %v", err)
	}
}

func TestDurableSpoolReportsOldestAge(t *testing.T) {
	spool, err := NewDurableSpool(t.TempDir(), 4096, 256)
	if err != nil {
		t.Fatalf("new spool: %v", err)
	}
	created := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	spool.now = func() time.Time { return created }
	if _, err := spool.Enqueue("event-age", SpoolMetadata, []byte("event")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	spool.now = func() time.Time { return created.Add(10 * time.Minute) }
	metrics := spool.Metrics()
	if metrics.Depth != 1 || metrics.OldestAge != 10*time.Minute {
		t.Fatalf("unexpected age metrics: %+v", metrics)
	}
}
