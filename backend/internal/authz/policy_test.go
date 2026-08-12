package authz

import (
	"errors"
	"testing"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

func principal(role identity.Role, scopes ...Capability) identity.Principal {
	values := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		values = append(values, string(scope))
	}
	return identity.Principal{
		UserID: "user-1", TenantID: "tenant-a", SiteIDs: []string{"site-a"},
		Roles: []identity.Role{role}, Scopes: values,
		DataClasses: []string{"metadata", "raw_video", "biometric"}, MFALevel: "t2",
	}
}

func TestFiveSeedRolesHaveExpectedLeastPrivilege(t *testing.T) {
	tests := []struct {
		name       string
		principal  identity.Principal
		capability Capability
		allowed    bool
	}{
		{"super admin manages tenant", principal(identity.RoleSuperAdmin, CapabilityTenantManage), CapabilityTenantManage, true},
		{"site admin cannot manage tenant", principal(identity.RoleSiteAdmin, CapabilityTenantManage), CapabilityTenantManage, false},
		{"operator handles alerts", principal(identity.RoleOperator, CapabilityAlertsWrite), CapabilityAlertsWrite, true},
		{"auditor reads audit", principal(identity.RoleAuditor, CapabilityAuditRead), CapabilityAuditRead, true},
		{"auditor cannot handle alerts", principal(identity.RoleAuditor, CapabilityAlertsWrite), CapabilityAlertsWrite, false},
		{"viewer reads analytics", principal(identity.RoleViewer, CapabilityAnalyticsRead), CapabilityAnalyticsRead, true},
		{"viewer cannot read raw video", principal(identity.RoleViewer, CapabilityRawVideoRead), CapabilityRawVideoRead, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Authorize(test.principal, Request{Capability: test.capability, TenantID: "tenant-a", SiteID: "site-a"})
			if test.allowed && err != nil {
				t.Fatalf("expected allow, got %v", err)
			}
			if !test.allowed && !errors.Is(err, ErrDenied) {
				t.Fatalf("expected deny, got %v", err)
			}
		})
	}
}

func TestAuthorizationRequiresEveryBoundary(t *testing.T) {
	base := principal(identity.RoleOperator, CapabilityAlertsWrite)
	tests := []struct {
		name      string
		principal identity.Principal
		request   Request
	}{
		{"cross tenant", base, Request{Capability: CapabilityAlertsWrite, TenantID: "tenant-b", SiteID: "site-a"}},
		{"cross site", base, Request{Capability: CapabilityAlertsWrite, TenantID: "tenant-a", SiteID: "site-b"}},
		{"missing scope", principal(identity.RoleOperator), Request{Capability: CapabilityAlertsWrite, TenantID: "tenant-a", SiteID: "site-a"}},
		{"unknown role", principal(identity.Role("unknown"), CapabilityAlertsWrite), Request{Capability: CapabilityAlertsWrite, TenantID: "tenant-a", SiteID: "site-a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Authorize(test.principal, test.request); !errors.Is(err, ErrDenied) {
				t.Fatalf("expected deny, got %v", err)
			}
		})
	}
}

func TestMFAEnforcement(t *testing.T) {
	operator := principal(identity.RoleOperator, CapabilityEvidenceExport)
	operator.MFALevel = "password"
	if err := Authorize(operator, Request{Capability: CapabilityEvidenceExport, TenantID: "tenant-a", SiteID: "site-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("sensitive capability should require MFA, got %v", err)
	}

	auditor := principal(identity.RoleAuditor, CapabilityAuditRead)
	auditor.MFALevel = "none"
	if err := Authorize(auditor, Request{Capability: CapabilityAuditRead, TenantID: "tenant-a", SiteID: "site-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("auditor token without MFA should be denied, got %v", err)
	}
}

func TestSensitiveDataClassEnforcement(t *testing.T) {
	operator := principal(identity.RoleOperator, CapabilityRawVideoRead)
	operator.DataClasses = []string{"metadata"}
	if err := Authorize(operator, Request{Capability: CapabilityRawVideoRead, TenantID: "tenant-a", SiteID: "site-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("raw-video capability should require raw_video data class, got %v", err)
	}

	superAdmin := principal(identity.RoleSuperAdmin, CapabilityDataErase)
	if err := Authorize(superAdmin, Request{Capability: CapabilityDataErase, TenantID: "tenant-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("data erasure must remain denied until dual approval is implemented, got %v", err)
	}
}

func TestCanAccessSite(t *testing.T) {
	operator := principal(identity.RoleOperator)
	if !CanAccessSite(operator, "tenant-a", "site-a") || CanAccessSite(operator, "tenant-a", "site-b") {
		t.Fatal("site-scoped role containment failed")
	}
	superAdmin := principal(identity.RoleSuperAdmin)
	if !CanAccessSite(superAdmin, "tenant-a", "site-b") || CanAccessSite(superAdmin, "tenant-b", "site-b") {
		t.Fatal("tenant-wide role containment failed")
	}
}
