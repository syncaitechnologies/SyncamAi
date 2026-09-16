package privacyrights

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testCaseStore(t *testing.T) *TestOnlyCaseStore {
	t.Helper()
	store, err := NewTestOnlyCaseStore(testCaseDraft(t), validIntakePolicy())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testTransitionCommand(requestID string, expectedVersion int64, nextState string, transitionedAt time.Time) TransitionCommand {
	return TransitionCommand{RequestID: requestID, ExpectedVersion: expectedVersion, NextState: nextState, TransitionedAt: transitionedAt}
}

func TestTestOnlyCaseStoreReplaysExactTransitionAndPreservesOriginalResult(t *testing.T) {
	store := testCaseStore(t)
	initial, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	command := testTransitionCommand("55555555-5555-4555-8555-555555555555", initial.Version, UnderReview, initial.UpdatedAt.Add(time.Minute))
	accepted, err := store.Apply(command)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := store.Apply(command)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Replayed || !replayed.Replayed || accepted.Draft != replayed.Draft || accepted.Draft.State != UnderReview || accepted.Draft.Version != 2 {
		t.Fatalf("exact replay must preserve its original result: accepted=%#v replayed=%#v", accepted, replayed)
	}
	next, err := store.Apply(testTransitionCommand("66666666-6666-4666-8666-666666666666", accepted.Draft.Version, FulfillmentPending, accepted.Draft.UpdatedAt.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if next.Replayed || next.Draft.State != FulfillmentPending || next.Draft.Version != 3 {
		t.Fatalf("subsequent transition: %#v", next)
	}
	replayed, err = store.Apply(command)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Draft != accepted.Draft {
		t.Fatalf("late replay must return the original receipt: %#v", replayed)
	}
}

func TestTestOnlyCaseStoreRejectsConflictingReuseAndStaleExpectedVersion(t *testing.T) {
	store := testCaseStore(t)
	initial, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	command := testTransitionCommand("55555555-5555-4555-8555-555555555555", initial.Version, UnderReview, initial.UpdatedAt.Add(time.Minute))
	if _, err := store.Apply(command); err != nil {
		t.Fatal(err)
	}
	conflicting := command
	conflicting.NextState = Rejected
	if _, err := store.Apply(conflicting); !errors.Is(err, ErrTransitionCommandConflict) {
		t.Fatalf("conflicting request reuse: %v", err)
	}
	if _, err := store.Apply(testTransitionCommand("66666666-6666-4666-8666-666666666666", initial.Version, FulfillmentPending, initial.UpdatedAt.Add(2*time.Minute))); !errors.Is(err, ErrTransitionVersionConflict) {
		t.Fatalf("stale expected version: %v", err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != UnderReview || snapshot.Version != 2 {
		t.Fatalf("failed mutations must preserve accepted draft: %#v", snapshot)
	}
}

func TestTestOnlyCaseStoreSerializesConcurrentExpectedVersion(t *testing.T) {
	store := testCaseStore(t)
	initial, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	commands := []TransitionCommand{
		testTransitionCommand("55555555-5555-4555-8555-555555555555", initial.Version, UnderReview, initial.UpdatedAt.Add(time.Minute)),
		testTransitionCommand("66666666-6666-4666-8666-666666666666", initial.Version, Rejected, initial.UpdatedAt.Add(time.Minute)),
	}
	start := make(chan struct{})
	errorsOut := make(chan error, len(commands))
	var wait sync.WaitGroup
	for _, command := range commands {
		command := command
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.Apply(command)
			errorsOut <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsOut)

	accepted, stale := 0, 0
	for err := range errorsOut {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrTransitionVersionConflict) {
			stale++
		} else {
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if accepted != 1 || stale != 1 {
		t.Fatalf("concurrent optimistic transition outcome: accepted=%d stale=%d", accepted, stale)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != 2 || (snapshot.State != UnderReview && snapshot.State != Rejected) {
		t.Fatalf("concurrent transition committed more than once: %#v", snapshot)
	}
}

func TestTestOnlyCaseStoreFailsClosedForInvalidConstructionAndCommands(t *testing.T) {
	draft := testCaseDraft(t)
	invalidDraft := draft
	invalidDraft.State = "completed_without_evidence"
	if _, err := NewTestOnlyCaseStore(invalidDraft, validIntakePolicy()); !errors.Is(err, ErrInvalidTestOnlyCaseStore) {
		t.Fatalf("invalid initial draft: %v", err)
	}
	if _, err := (*TestOnlyCaseStore)(nil).Snapshot(); !errors.Is(err, ErrInvalidTestOnlyCaseStore) {
		t.Fatalf("nil snapshot: %v", err)
	}
	store := testCaseStore(t)
	for _, command := range []TransitionCommand{
		testTransitionCommand("not-a-uuid", draft.Version, UnderReview, draft.UpdatedAt.Add(time.Minute)),
		testTransitionCommand("55555555-5555-4555-8555-555555555555", 0, UnderReview, draft.UpdatedAt.Add(time.Minute)),
		testTransitionCommand("55555555-5555-4555-8555-555555555555", draft.Version, "", draft.UpdatedAt.Add(time.Minute)),
		testTransitionCommand("55555555-5555-4555-8555-555555555555", draft.Version, UnderReview, time.Time{}),
	} {
		if _, err := store.Apply(command); !errors.Is(err, ErrInvalidTestOnlyCaseStore) {
			t.Fatalf("invalid command must fail closed: %v", err)
		}
	}
}
