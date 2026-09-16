package privacyrights

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidTestOnlyCaseStore  = errors.New("test-only privacy rights case store is invalid")
	ErrTransitionCommandConflict = errors.New("privacy rights transition request identifier was reused with different details")
	ErrTransitionVersionConflict = errors.New("privacy rights transition expected version does not match")
)

// TransitionCommand is a bounded synthetic mutation request. It contains no
// requester narrative, evidence, consent, biometric data, or fulfillment data.
type TransitionCommand struct {
	RequestID       string
	ExpectedVersion int64
	NextState       string
	TransitionedAt  time.Time
}

// TransitionResult reports the immutable result of one accepted synthetic
// transition. Replayed means the exact request identifier was already applied.
type TransitionResult struct {
	Draft    CaseDraft
	Replayed bool
}

type transitionReceipt struct {
	command TransitionCommand
	draft   CaseDraft
}

// TestOnlyCaseStore serializes one synthetic case draft in process. It is not a
// persistent repository or production concurrency mechanism.
type TestOnlyCaseStore struct {
	mu       sync.Mutex
	policy   TestPolicy
	draft    CaseDraft
	receipts map[string]transitionReceipt
}

func NewTestOnlyCaseStore(draft CaseDraft, policy TestPolicy) (*TestOnlyCaseStore, error) {
	if !validTestCaseDraft(draft, policy) {
		return nil, ErrInvalidTestOnlyCaseStore
	}
	return &TestOnlyCaseStore{
		policy:   policy,
		draft:    draft,
		receipts: make(map[string]transitionReceipt),
	}, nil
}

// Apply accepts one optimistic, idempotent test-only transition. An exact
// replay returns its original result even after the current draft has advanced.
func (s *TestOnlyCaseStore) Apply(command TransitionCommand) (TransitionResult, error) {
	if s == nil || !validTransitionCommand(command) {
		return TransitionResult{}, ErrInvalidTestOnlyCaseStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if receipt, found := s.receipts[command.RequestID]; found {
		if !sameTransitionCommand(receipt.command, command) {
			return TransitionResult{}, ErrTransitionCommandConflict
		}
		return TransitionResult{Draft: receipt.draft, Replayed: true}, nil
	}
	if command.ExpectedVersion != s.draft.Version {
		return TransitionResult{}, ErrTransitionVersionConflict
	}
	next, err := TransitionTestDraft(s.draft, s.policy, command.NextState, command.TransitionedAt)
	if err != nil {
		return TransitionResult{}, err
	}
	s.draft = next
	s.receipts[command.RequestID] = transitionReceipt{command: command, draft: next}
	return TransitionResult{Draft: next}, nil
}

// Snapshot returns a metadata-only copy of the current synthetic draft.
func (s *TestOnlyCaseStore) Snapshot() (CaseDraft, error) {
	if s == nil {
		return CaseDraft{}, ErrInvalidTestOnlyCaseStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draft, nil
}

func validTransitionCommand(command TransitionCommand) bool {
	return validUUIDv4(command.RequestID) && command.ExpectedVersion >= 1 && command.NextState != "" && !command.TransitionedAt.IsZero() && command.TransitionedAt.Location() == time.UTC
}

func sameTransitionCommand(left, right TransitionCommand) bool {
	return left.RequestID == right.RequestID && left.ExpectedVersion == right.ExpectedVersion && left.NextState == right.NextState && left.TransitionedAt.Equal(right.TransitionedAt)
}
