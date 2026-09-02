// Package usermanagement defines the server-side boundary for tenant user
// lifecycle changes. It intentionally has no browser-facing or provider
// implementation until the administrator bootstrap and recovery policy is
// approved.
package usermanagement

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

var ErrLifecycleUnavailable = errors.New("user lifecycle adapter is not configured")

// Adapter is a server-only integration point for a future identity provider.
// Implementations must never be reachable from browser credentials. Before an
// adapter is wired, its design must preserve a verified transaction-local
// app.tenant_id, and atomically record the requested membership change, audit
// event, and outbox message. Provider delivery must be recoverable from that
// outbox; a remote identity-provider call is not a substitute for the local
// transactional boundary.
type Adapter interface {
	Invite(context.Context, InviteRequest) (Invitation, error)
	Disable(context.Context, DisableRequest) error
	Reassign(context.Context, ReassignRequest) error
}

// Service is the only intended entry point for lifecycle adapters. It derives
// the actor from a verified principal and enforces the tenant-wide
// users:manage capability before an adapter can be called.
type Service struct{ adapter Adapter }

func New(adapter Adapter) Service { return Service{adapter: adapter} }

// InviteCommand does not select roles or scopes. Role assignment is a separate
// security-reviewed concern (T-0067), not an implicit side effect of inviting
// a user.
type InviteCommand struct {
	TenantID  string
	RequestID string
	Email     string
}

// InviteRequest is passed to a server-side Adapter only after Service has
// checked the verified actor, tenant, capability, and request values.
type InviteRequest struct {
	InviteCommand
	ActorID string
}

// Invitation describes a durable request only. Queued is true when delivery
// has not yet been attempted by the separate provider worker.
type Invitation struct {
	ID     string
	Email  string
	Queued bool
}

type DisableCommand struct {
	TenantID  string
	RequestID string
	UserID    string
}

type DisableRequest struct {
	DisableCommand
	ActorID string
}

// ReassignCommand changes only a future user's site assignment. It does not
// grant roles, scopes, or data classifications.
type ReassignCommand struct {
	TenantID  string
	RequestID string
	UserID    string
	SiteIDs   []string
}

type ReassignRequest struct {
	ReassignCommand
	ActorID string
}

func (s Service) Invite(ctx context.Context, principal identity.Principal, command InviteCommand) (Invitation, error) {
	if err := authorize(principal, command.TenantID); err != nil {
		return Invitation{}, err
	}
	if err := validateInvite(command); err != nil {
		return Invitation{}, err
	}
	if s.adapter == nil {
		return Invitation{}, ErrLifecycleUnavailable
	}
	return s.adapter.Invite(ctx, InviteRequest{InviteCommand: command, ActorID: principal.UserID})
}

func (s Service) Disable(ctx context.Context, principal identity.Principal, command DisableCommand) error {
	if err := authorize(principal, command.TenantID); err != nil {
		return err
	}
	if err := validateDisable(command); err != nil {
		return err
	}
	if s.adapter == nil {
		return ErrLifecycleUnavailable
	}
	return s.adapter.Disable(ctx, DisableRequest{DisableCommand: command, ActorID: principal.UserID})
}

func (s Service) Reassign(ctx context.Context, principal identity.Principal, command ReassignCommand) error {
	if err := authorize(principal, command.TenantID); err != nil {
		return err
	}
	if err := validateReassign(command); err != nil {
		return err
	}
	if s.adapter == nil {
		return ErrLifecycleUnavailable
	}
	copyCommand := command
	copyCommand.SiteIDs = append([]string(nil), command.SiteIDs...)
	return s.adapter.Reassign(ctx, ReassignRequest{ReassignCommand: copyCommand, ActorID: principal.UserID})
}

func authorize(principal identity.Principal, tenantID string) error {
	return authz.Authorize(principal, authz.Request{
		Capability: authz.CapabilityUsersManage,
		TenantID:   strings.TrimSpace(tenantID),
	})
}

func validateInvite(command InviteCommand) error {
	if err := validateEnvelope(command.TenantID, command.RequestID); err != nil {
		return err
	}
	email := strings.TrimSpace(command.Email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 320 {
		return errors.New("invite email is invalid")
	}
	return nil
}

func validateDisable(command DisableCommand) error {
	if err := validateEnvelope(command.TenantID, command.RequestID); err != nil {
		return err
	}
	if _, err := uuid.Parse(strings.TrimSpace(command.UserID)); err != nil {
		return errors.New("target user identifier must be a UUID")
	}
	return nil
}

func validateReassign(command ReassignCommand) error {
	if err := validateDisable(DisableCommand{TenantID: command.TenantID, RequestID: command.RequestID, UserID: command.UserID}); err != nil {
		return err
	}
	if len(command.SiteIDs) == 0 || len(command.SiteIDs) > 500 {
		return errors.New("at least one and no more than 500 site assignments are required")
	}
	seen := make(map[string]struct{}, len(command.SiteIDs))
	for _, siteID := range command.SiteIDs {
		siteID = strings.TrimSpace(siteID)
		if _, err := uuid.Parse(siteID); err != nil {
			return errors.New("site assignment identifier must be a UUID")
		}
		if _, exists := seen[siteID]; exists {
			return errors.New("site assignments must be unique")
		}
		seen[siteID] = struct{}{}
	}
	return nil
}

func validateEnvelope(tenantID, requestID string) error {
	if _, err := uuid.Parse(strings.TrimSpace(tenantID)); err != nil {
		return errors.New("verified tenant identifier must be a UUID")
	}
	parsed, err := uuid.Parse(strings.TrimSpace(requestID))
	if err != nil || parsed.Version() != 4 {
		return errors.New("audit request identifier must be a UUIDv4")
	}
	return nil
}
