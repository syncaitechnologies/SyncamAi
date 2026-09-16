// Package privacyrights defines the test-only metadata boundary for a future
// privacy-rights intake workflow. It does not persist, route, verify, fulfill,
// or otherwise process a request.
package privacyrights

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	TestOnlyPolicyMode = "test_only"

	maxPolicyIdentifierLength = 64
	maxTestDeadline           = 366 * 24 * time.Hour
)

var ErrInvalidIntakeDraft = errors.New("privacy rights intake draft is invalid")

// RequestKind is a bounded category from the draft individual privacy-request
// procedure. A draft records a requested category only; it makes no legal
// determination or fulfills the request.
type RequestKind string

const (
	RequestAccess      RequestKind = "access"
	RequestCorrection  RequestKind = "correction"
	RequestErasure     RequestKind = "erasure"
	RequestWithdrawal  RequestKind = "withdrawal"
	RequestGrievance   RequestKind = "grievance"
	RequestNomination  RequestKind = "nomination"
	RequestPortability RequestKind = "portability"
	RequestOptOut      RequestKind = "opt_out"
	RequestLimitation  RequestKind = "limitation"

	VerificationPending = "verification_pending"
)

// TestPolicy is caller-supplied synthetic policy metadata. The production
// legal clock, jurisdiction, purpose and policy versions require separate
// approved configuration; this package rejects every non-test policy mode.
type TestPolicy struct {
	Mode             string
	JurisdictionID   string
	PurposeID        string
	PolicyVersion    string
	LegalClockID     string
	DeadlineInterval time.Duration
}

// DraftInput uses opaque UUIDs only. RequesterID is optional so a later secure
// private intake channel can support visitors and former employees without an
// application account. No name, email, narrative, evidence, document, consent,
// biometric or other personal payload is accepted here.
type DraftInput struct {
	CaseID      string
	TenantID    string
	SubjectID   string
	RequesterID string
	Kind        RequestKind
}

// CaseDraft is unpersisted metadata for test verification of a future intake
// contract. It has no fulfillment or completion state.
type CaseDraft struct {
	CaseID         string
	TenantID       string
	SubjectID      string
	RequesterID    string
	Kind           RequestKind
	State          string
	Version        int64
	ReceivedAt     time.Time
	DueAt          time.Time
	JurisdictionID string
	PurposeID      string
	PolicyVersion  string
	LegalClockID   string
}

// CreateTestDraft validates a bounded metadata-only draft under an injected
// synthetic policy. It never writes data, calls a provider, changes consent,
// starts deletion, assigns an owner, or emits an audit/outbox event.
func CreateTestDraft(input DraftInput, policy TestPolicy, receivedAt time.Time) (CaseDraft, error) {
	if !validDraftInput(input) || !validTestPolicy(policy) || receivedAt.IsZero() || receivedAt.Location() != time.UTC {
		return CaseDraft{}, ErrInvalidIntakeDraft
	}
	return CaseDraft{
		CaseID:         input.CaseID,
		TenantID:       input.TenantID,
		SubjectID:      input.SubjectID,
		RequesterID:    input.RequesterID,
		Kind:           input.Kind,
		State:          VerificationPending,
		Version:        1,
		ReceivedAt:     receivedAt,
		DueAt:          receivedAt.Add(policy.DeadlineInterval),
		JurisdictionID: policy.JurisdictionID,
		PurposeID:      policy.PurposeID,
		PolicyVersion:  policy.PolicyVersion,
		LegalClockID:   policy.LegalClockID,
	}, nil
}

func validDraftInput(input DraftInput) bool {
	if !validUUIDv4(input.CaseID) || !validUUIDv4(input.TenantID) || !validUUIDv4(input.SubjectID) || !validRequestKind(input.Kind) {
		return false
	}
	return input.RequesterID == "" || validUUIDv4(input.RequesterID)
}

func validTestPolicy(policy TestPolicy) bool {
	if policy.Mode != TestOnlyPolicyMode || policy.DeadlineInterval <= 0 || policy.DeadlineInterval > maxTestDeadline {
		return false
	}
	return validPolicyIdentifier(policy.JurisdictionID) && validPolicyIdentifier(policy.PurposeID) && validPolicyIdentifier(policy.PolicyVersion) && validPolicyIdentifier(policy.LegalClockID)
}

func validUUIDv4(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 4
}

func validRequestKind(kind RequestKind) bool {
	switch kind {
	case RequestAccess, RequestCorrection, RequestErasure, RequestWithdrawal, RequestGrievance, RequestNomination, RequestPortability, RequestOptOut, RequestLimitation:
		return true
	default:
		return false
	}
}

func validPolicyIdentifier(value string) bool {
	if value != strings.TrimSpace(value) || value == "" || len(value) > maxPolicyIdentifierLength {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}
