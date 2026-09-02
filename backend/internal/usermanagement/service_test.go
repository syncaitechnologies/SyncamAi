package usermanagement

import (
	"context"
	"errors"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/authz"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

const (
	userTenant   = "11111111-1111-4111-8111-111111111111"
	userID       = "22222222-2222-4222-8222-222222222222"
	firstSiteID  = "33333333-3333-4333-8333-333333333333"
	secondSiteID = "44444444-4444-4444-8444-444444444444"
	userRequest  = "55555555-5555-4555-8555-555555555555"
)

type adapterStub struct {
	invited   []InviteRequest
	disabled  []DisableRequest
	reassigned []ReassignRequest
	err       error
}

func (a *adapterStub) Invite(_ context.Context, request InviteRequest) (Invitation, error) {
	a.invited = append(a.invited, request)
	if a.err != nil {
		return Invitation{}, a.err
	}
	return Invitation{ID: "invite-1", Email: request.Email}, nil
}

func (a *adapterStub) Disable(_ context.Context, request DisableRequest) error {
	a.disabled = append(a.disabled, request)
	return a.err
}

func (a *adapterStub) Reassign(_ context.Context, request ReassignRequest) error {
	a.reassigned = append(a.reassigned, request)
	return a.err
}

func managementPrincipal(role identity.Role, scope bool, mfa string) identity.Principal {
	scopes := []string{}
	if scope {
		scopes = append(scopes, string(authz.CapabilityUsersManage))
	}
	return identity.Principal{
		UserID: "actor-1", TenantID: userTenant, Roles: []identity.Role{role},
		Scopes: scopes, DataClasses: []string{"metadata"}, MFALevel: mfa,
	}
}

func inviteCommand() InviteCommand {
	return InviteCommand{TenantID: userTenant, RequestID: userRequest, Email: "new.user@example.test"}
}

func disableCommand() DisableCommand {
	return DisableCommand{TenantID: userTenant, RequestID: userRequest, UserID: userID}
}

func reassignCommand() ReassignCommand {
	return ReassignCommand{TenantID: userTenant, RequestID: userRequest, UserID: userID, SiteIDs: []string{firstSiteID, secondSiteID}}
}

func TestServiceDeniesEveryUnauthorizedLifecycleCall(t *testing.T) {
	tests := []struct {
		name      string
		principal identity.Principal
	}{
		{"site admin has no tenant-wide grant", managementPrincipal(identity.RoleSiteAdmin, true, "aal2")},
		{"super admin needs explicit scope", managementPrincipal(identity.RoleSuperAdmin, false, "aal2")},
		{"super admin needs MFA", managementPrincipal(identity.RoleSuperAdmin, true, "password")},
		{"cross tenant", managementPrincipal(identity.RoleSuperAdmin, true, "aal2")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := &adapterStub{}
			command := inviteCommand()
			if test.name == "cross tenant" {
				command.TenantID = secondSiteID
			}
			_, err := New(adapter).Invite(context.Background(), test.principal, command)
			if !errors.Is(err, authz.ErrDenied) {
				t.Fatalf("expected authorization denial, got %v", err)
			}
			if len(adapter.invited) != 0 {
				t.Fatal("adapter must not be called before authorization succeeds")
			}
		})
	}
}

func TestServiceFailsClosedUntilAnAdapterIsConfigured(t *testing.T) {
	principal := managementPrincipal(identity.RoleSuperAdmin, true, "aal2")
	service := New(nil)
	if _, err := service.Invite(context.Background(), principal, inviteCommand()); !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("invite: expected unavailable adapter, got %v", err)
	}
	if err := service.Disable(context.Background(), principal, disableCommand()); !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("disable: expected unavailable adapter, got %v", err)
	}
	if err := service.Reassign(context.Background(), principal, reassignCommand()); !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("reassign: expected unavailable adapter, got %v", err)
	}
}

func TestServiceValidatesBeforeCallingAnAdapter(t *testing.T) {
	principal := managementPrincipal(identity.RoleSuperAdmin, true, "aal2")
	adapter := &adapterStub{}
	service := New(adapter)

	invalidInvite := inviteCommand()
	invalidInvite.Email = "not-an-email"
	if _, err := service.Invite(context.Background(), principal, invalidInvite); err == nil {
		t.Fatal("invalid invite must fail")
	}
	invalidDisable := disableCommand()
	invalidDisable.UserID = "bad"
	if err := service.Disable(context.Background(), principal, invalidDisable); err == nil {
		t.Fatal("invalid disable must fail")
	}
	invalidReassign := reassignCommand()
	invalidReassign.SiteIDs = []string{firstSiteID, firstSiteID}
	if err := service.Reassign(context.Background(), principal, invalidReassign); err == nil {
		t.Fatal("duplicate site assignments must fail")
	}
	if len(adapter.invited) != 0 || len(adapter.disabled) != 0 || len(adapter.reassigned) != 0 {
		t.Fatal("adapter must not receive invalid lifecycle requests")
	}
}

func TestServicePassesVerifiedActorAndCopiesSiteAssignments(t *testing.T) {
	principal := managementPrincipal(identity.RoleSuperAdmin, true, "aal2")
	adapter := &adapterStub{}
	service := New(adapter)
	invitation, err := service.Invite(context.Background(), principal, inviteCommand())
	if err != nil || invitation.Email != "new.user@example.test" || len(adapter.invited) != 1 || adapter.invited[0].ActorID != principal.UserID {
		t.Fatalf("invite adapter boundary failed: %#v %#v %v", invitation, adapter.invited, err)
	}
	if err := service.Disable(context.Background(), principal, disableCommand()); err != nil || len(adapter.disabled) != 1 || adapter.disabled[0].ActorID != principal.UserID {
		t.Fatalf("disable adapter boundary failed: %#v %v", adapter.disabled, err)
	}
	command := reassignCommand()
	if err := service.Reassign(context.Background(), principal, command); err != nil || len(adapter.reassigned) != 1 || adapter.reassigned[0].ActorID != principal.UserID {
		t.Fatalf("reassign adapter boundary failed: %#v %v", adapter.reassigned, err)
	}
	command.SiteIDs[0] = secondSiteID
	if adapter.reassigned[0].SiteIDs[0] != firstSiteID {
		t.Fatal("adapter request must not retain caller-owned site assignments")
	}
}

func TestServicePreservesAdapterFailures(t *testing.T) {
	providerFailure := errors.New("provider unavailable")
	service := New(&adapterStub{err: providerFailure})
	principal := managementPrincipal(identity.RoleSuperAdmin, true, "aal2")
	if _, err := service.Invite(context.Background(), principal, inviteCommand()); !errors.Is(err, providerFailure) {
		t.Fatalf("expected provider failure, got %v", err)
	}
}
