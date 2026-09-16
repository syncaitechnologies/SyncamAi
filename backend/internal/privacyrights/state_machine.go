package privacyrights

import "time"

// TransitionTestDraft advances a valid test-only case draft by one allowed
// workflow step. It does not persist the result, verify a requester, assign an
// owner, fulfill a request, modify consent, delete data, or make a legal
// determination.
func TransitionTestDraft(draft CaseDraft, policy TestPolicy, nextState string, transitionedAt time.Time) (CaseDraft, error) {
	if !validTestCaseDraft(draft, policy) || !validTransition(draft.State, nextState) || transitionedAt.IsZero() || transitionedAt.Location() != time.UTC || !transitionedAt.After(draft.UpdatedAt) {
		return CaseDraft{}, ErrInvalidIntakeDraft
	}
	updated := draft
	updated.State = nextState
	updated.Version++
	updated.UpdatedAt = transitionedAt
	return updated, nil
}

func validTestCaseDraft(draft CaseDraft, policy TestPolicy) bool {
	if !validDraftInput(DraftInput{
		CaseID:      draft.CaseID,
		TenantID:    draft.TenantID,
		SubjectID:   draft.SubjectID,
		RequesterID: draft.RequesterID,
		Kind:        draft.Kind,
	}) || !validTestPolicy(policy) || !validCaseState(draft.State) || draft.Version < 1 {
		return false
	}
	if draft.JurisdictionID != policy.JurisdictionID || draft.PurposeID != policy.PurposeID || draft.PolicyVersion != policy.PolicyVersion || draft.LegalClockID != policy.LegalClockID {
		return false
	}
	if draft.ReceivedAt.IsZero() || draft.UpdatedAt.IsZero() || draft.DueAt.IsZero() || draft.ReceivedAt.Location() != time.UTC || draft.UpdatedAt.Location() != time.UTC || draft.DueAt.Location() != time.UTC || draft.UpdatedAt.Before(draft.ReceivedAt) {
		return false
	}
	return draft.DueAt.Equal(draft.ReceivedAt.Add(policy.DeadlineInterval))
}

func validTransition(currentState, nextState string) bool {
	switch currentState {
	case VerificationPending:
		return nextState == UnderReview || nextState == Rejected
	case UnderReview:
		return nextState == FulfillmentPending || nextState == Rejected
	case FulfillmentPending:
		return nextState == Completed || nextState == PartiallyFulfilled
	default:
		return false
	}
}

func validCaseState(state string) bool {
	switch state {
	case VerificationPending, UnderReview, FulfillmentPending, Completed, Rejected, PartiallyFulfilled:
		return true
	default:
		return false
	}
}
