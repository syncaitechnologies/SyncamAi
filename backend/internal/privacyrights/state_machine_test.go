package privacyrights

import (
	"errors"
	"testing"
	"time"
)

func testCaseDraft(t *testing.T) CaseDraft {
	t.Helper()
	receivedAt := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)
	draft, err := CreateTestDraft(validTestDraftInput(RequestErasure), validIntakePolicy(), receivedAt)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestTransitionTestDraftAllowsOnlyOrderedPendingReviewFulfillmentCompletion(t *testing.T) {
	draft := testCaseDraft(t)
	underReview, err := TransitionTestDraft(draft, validIntakePolicy(), UnderReview, draft.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	fulfillment, err := TransitionTestDraft(underReview, validIntakePolicy(), FulfillmentPending, underReview.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	completed, err := TransitionTestDraft(fulfillment, validIntakePolicy(), Completed, fulfillment.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if draft.State != VerificationPending || draft.Version != 1 || !draft.UpdatedAt.Equal(draft.ReceivedAt) {
		t.Fatalf("transition must not mutate the input draft: %#v", draft)
	}
	if underReview.State != UnderReview || underReview.Version != 2 || fulfillment.State != FulfillmentPending || fulfillment.Version != 3 || completed.State != Completed || completed.Version != 4 {
		t.Fatalf("unexpected ordered transition result: review=%#v fulfillment=%#v completed=%#v", underReview, fulfillment, completed)
	}
	if !completed.DueAt.Equal(draft.DueAt) || completed.JurisdictionID != draft.JurisdictionID || completed.PurposeID != draft.PurposeID || completed.PolicyVersion != draft.PolicyVersion || completed.LegalClockID != draft.LegalClockID {
		t.Fatalf("transition must preserve immutable policy metadata: %#v", completed)
	}
}

func TestTransitionTestDraftAllowsOnlyDocumentedTerminalOutcomes(t *testing.T) {
	draft := testCaseDraft(t)
	if rejected, err := TransitionTestDraft(draft, validIntakePolicy(), Rejected, draft.UpdatedAt.Add(time.Minute)); err != nil || rejected.State != Rejected || rejected.Version != 2 {
		t.Fatalf("verification rejection: draft=%#v err=%v", rejected, err)
	}
	underReview, err := TransitionTestDraft(draft, validIntakePolicy(), UnderReview, draft.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if rejected, err := TransitionTestDraft(underReview, validIntakePolicy(), Rejected, underReview.UpdatedAt.Add(time.Minute)); err != nil || rejected.State != Rejected || rejected.Version != 3 {
		t.Fatalf("review rejection: draft=%#v err=%v", rejected, err)
	}
	fulfillment, err := TransitionTestDraft(underReview, validIntakePolicy(), FulfillmentPending, underReview.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if partial, err := TransitionTestDraft(fulfillment, validIntakePolicy(), PartiallyFulfilled, fulfillment.UpdatedAt.Add(time.Minute)); err != nil || partial.State != PartiallyFulfilled || partial.Version != 4 {
		t.Fatalf("partial fulfillment: draft=%#v err=%v", partial, err)
	}
}

func TestTransitionTestDraftFailsClosedForSkippedTerminalStaleAndMismatchedDrafts(t *testing.T) {
	draft := testCaseDraft(t)
	underReview, err := TransitionTestDraft(draft, validIntakePolicy(), UnderReview, draft.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	fulfillment, err := TransitionTestDraft(underReview, validIntakePolicy(), FulfillmentPending, underReview.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	completed, err := TransitionTestDraft(fulfillment, validIntakePolicy(), Completed, fulfillment.UpdatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	mismatchedPolicy := validIntakePolicy()
	mismatchedPolicy.PurposeID = "other-purpose"
	invalidDueAt := draft
	invalidDueAt.DueAt = invalidDueAt.DueAt.Add(time.Minute)
	invalidState := draft
	invalidState.State = "fulfilled"
	invalidVersion := draft
	invalidVersion.Version = 0

	for _, test := range []struct {
		name       string
		draft      CaseDraft
		policy     TestPolicy
		nextState  string
		transition time.Time
	}{
		{name: "skipped review", draft: draft, policy: validIntakePolicy(), nextState: FulfillmentPending, transition: draft.UpdatedAt.Add(time.Minute)},
		{name: "same state", draft: underReview, policy: validIntakePolicy(), nextState: UnderReview, transition: underReview.UpdatedAt.Add(time.Minute)},
		{name: "rejection after fulfillment", draft: fulfillment, policy: validIntakePolicy(), nextState: Rejected, transition: fulfillment.UpdatedAt.Add(time.Minute)},
		{name: "terminal transition", draft: completed, policy: validIntakePolicy(), nextState: UnderReview, transition: completed.UpdatedAt.Add(time.Minute)},
		{name: "stale timestamp", draft: draft, policy: validIntakePolicy(), nextState: UnderReview, transition: draft.UpdatedAt},
		{name: "non-utc timestamp", draft: draft, policy: validIntakePolicy(), nextState: UnderReview, transition: time.Date(2026, time.September, 15, 10, 1, 0, 0, time.FixedZone("test", -4*60*60))},
		{name: "policy mismatch", draft: draft, policy: mismatchedPolicy, nextState: UnderReview, transition: draft.UpdatedAt.Add(time.Minute)},
		{name: "deadline mutation", draft: invalidDueAt, policy: validIntakePolicy(), nextState: UnderReview, transition: draft.UpdatedAt.Add(time.Minute)},
		{name: "unknown current state", draft: invalidState, policy: validIntakePolicy(), nextState: UnderReview, transition: draft.UpdatedAt.Add(time.Minute)},
		{name: "zero version", draft: invalidVersion, policy: validIntakePolicy(), nextState: UnderReview, transition: draft.UpdatedAt.Add(time.Minute)},
		{name: "production policy", draft: draft, policy: TestPolicy{Mode: "production", JurisdictionID: "synthetic-jurisdiction", PurposeID: "synthetic-purpose", PolicyVersion: "test-1.0", LegalClockID: "synthetic-clock", DeadlineInterval: 48 * time.Hour}, nextState: UnderReview, transition: draft.UpdatedAt.Add(time.Minute)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := TransitionTestDraft(test.draft, test.policy, test.nextState, test.transition); !errors.Is(err, ErrInvalidIntakeDraft) {
				t.Fatalf("invalid transition must fail closed: %v", err)
			}
		})
	}
}
