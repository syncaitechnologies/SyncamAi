package privacyrights

import (
	"errors"
	"testing"
	"time"
)

func validIntakePolicy() TestPolicy {
	return TestPolicy{
		Mode:             TestOnlyPolicyMode,
		JurisdictionID:   "synthetic-jurisdiction",
		PurposeID:        "synthetic-purpose",
		PolicyVersion:    "test-1.0",
		LegalClockID:     "synthetic-clock",
		DeadlineInterval: 48 * time.Hour,
	}
}

func validTestDraftInput(kind RequestKind) DraftInput {
	return DraftInput{
		CaseID:      "11111111-1111-4111-8111-111111111111",
		TenantID:    "22222222-2222-4222-8222-222222222222",
		SubjectID:   "33333333-3333-4333-8333-333333333333",
		RequesterID: "44444444-4444-4444-8444-444444444444",
		Kind:        kind,
	}
}

func TestCreateTestDraftBuildsOnlyVerificationPendingMetadata(t *testing.T) {
	receivedAt := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)
	draft, err := CreateTestDraft(validTestDraftInput(RequestErasure), validIntakePolicy(), receivedAt)
	if err != nil {
		t.Fatal(err)
	}
	if draft.CaseID != "11111111-1111-4111-8111-111111111111" || draft.TenantID != "22222222-2222-4222-8222-222222222222" || draft.SubjectID != "33333333-3333-4333-8333-333333333333" || draft.RequesterID != "44444444-4444-4444-8444-444444444444" {
		t.Fatalf("unexpected opaque identifiers: %#v", draft)
	}
	if draft.Kind != RequestErasure || draft.State != VerificationPending || draft.Version != 1 || !draft.ReceivedAt.Equal(receivedAt) || !draft.UpdatedAt.Equal(receivedAt) || !draft.DueAt.Equal(receivedAt.Add(48*time.Hour)) {
		t.Fatalf("draft must remain verification pending: %#v", draft)
	}
	if draft.JurisdictionID != "synthetic-jurisdiction" || draft.PurposeID != "synthetic-purpose" || draft.PolicyVersion != "test-1.0" || draft.LegalClockID != "synthetic-clock" {
		t.Fatalf("unexpected policy metadata: %#v", draft)
	}
}

func TestCreateTestDraftSupportsOnlyDocumentedRequestKinds(t *testing.T) {
	for _, kind := range []RequestKind{
		RequestAccess,
		RequestCorrection,
		RequestErasure,
		RequestWithdrawal,
		RequestGrievance,
		RequestNomination,
		RequestPortability,
		RequestOptOut,
		RequestLimitation,
	} {
		t.Run(string(kind), func(t *testing.T) {
			if _, err := CreateTestDraft(validTestDraftInput(kind), validIntakePolicy(), time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCreateTestDraftAllowsNoAccountRequesterReference(t *testing.T) {
	input := validTestDraftInput(RequestAccess)
	input.RequesterID = ""
	draft, err := CreateTestDraft(input, validIntakePolicy(), time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if draft.RequesterID != "" || draft.State != VerificationPending {
		t.Fatalf("non-account requester must remain pending verification: %#v", draft)
	}
}

func TestCreateTestDraftFailsClosedForInvalidInputOrPolicy(t *testing.T) {
	receivedAt := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)
	validInput := validTestDraftInput(RequestAccess)
	validPolicy := validIntakePolicy()

	tests := []struct {
		name     string
		input    DraftInput
		policy   TestPolicy
		received time.Time
	}{
		{name: "missing case", input: DraftInput{TenantID: validInput.TenantID, SubjectID: validInput.SubjectID, Kind: validInput.Kind}, policy: validPolicy, received: receivedAt},
		{name: "non-version-four tenant", input: DraftInput{CaseID: validInput.CaseID, TenantID: "22222222-2222-1222-8222-222222222222", SubjectID: validInput.SubjectID, Kind: validInput.Kind}, policy: validPolicy, received: receivedAt},
		{name: "invalid subject", input: DraftInput{CaseID: validInput.CaseID, TenantID: validInput.TenantID, SubjectID: "subject", Kind: validInput.Kind}, policy: validPolicy, received: receivedAt},
		{name: "non-opaque requester", input: DraftInput{CaseID: validInput.CaseID, TenantID: validInput.TenantID, SubjectID: validInput.SubjectID, RequesterID: "person@example.com", Kind: validInput.Kind}, policy: validPolicy, received: receivedAt},
		{name: "unknown kind", input: DraftInput{CaseID: validInput.CaseID, TenantID: validInput.TenantID, SubjectID: validInput.SubjectID, Kind: "delete_everything"}, policy: validPolicy, received: receivedAt},
		{name: "production policy mode", input: validInput, policy: TestPolicy{Mode: "production", JurisdictionID: validPolicy.JurisdictionID, PurposeID: validPolicy.PurposeID, PolicyVersion: validPolicy.PolicyVersion, LegalClockID: validPolicy.LegalClockID, DeadlineInterval: validPolicy.DeadlineInterval}, received: receivedAt},
		{name: "missing deadline", input: validInput, policy: TestPolicy{Mode: validPolicy.Mode, JurisdictionID: validPolicy.JurisdictionID, PurposeID: validPolicy.PurposeID, PolicyVersion: validPolicy.PolicyVersion, LegalClockID: validPolicy.LegalClockID}, received: receivedAt},
		{name: "unbounded deadline", input: validInput, policy: TestPolicy{Mode: validPolicy.Mode, JurisdictionID: validPolicy.JurisdictionID, PurposeID: validPolicy.PurposeID, PolicyVersion: validPolicy.PolicyVersion, LegalClockID: validPolicy.LegalClockID, DeadlineInterval: maxTestDeadline + time.Hour}, received: receivedAt},
		{name: "unsafe policy identifier", input: validInput, policy: TestPolicy{Mode: validPolicy.Mode, JurisdictionID: "Canada Ontario", PurposeID: validPolicy.PurposeID, PolicyVersion: validPolicy.PolicyVersion, LegalClockID: validPolicy.LegalClockID, DeadlineInterval: validPolicy.DeadlineInterval}, received: receivedAt},
		{name: "space-padded policy identifier", input: validInput, policy: TestPolicy{Mode: validPolicy.Mode, JurisdictionID: " synthetic-jurisdiction", PurposeID: validPolicy.PurposeID, PolicyVersion: validPolicy.PolicyVersion, LegalClockID: validPolicy.LegalClockID, DeadlineInterval: validPolicy.DeadlineInterval}, received: receivedAt},
		{name: "non-utc receipt", input: validInput, policy: validPolicy, received: time.Date(2026, time.September, 15, 10, 0, 0, 0, time.FixedZone("test", -4*60*60))},
		{name: "missing receipt", input: validInput, policy: validPolicy},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CreateTestDraft(test.input, test.policy, test.received); !errors.Is(err, ErrInvalidIntakeDraft) {
				t.Fatalf("invalid draft must fail closed: %v", err)
			}
		})
	}
}
