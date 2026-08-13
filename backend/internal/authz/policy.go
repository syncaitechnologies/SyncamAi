// Package authz enforces SyncCam AI's deny-by-default RBAC and site ABAC.
package authz

import (
	"errors"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
)

// Capability is an auditable server-side permission, also carried in scopes.
type Capability string

const (
	CapabilityAuthRead       Capability = "auth:read"
	CapabilitySitesRead      Capability = "sites:read"
	CapabilityTenantManage   Capability = "tenant:manage"
	CapabilitySiteManage     Capability = "site:manage"
	CapabilityConfigRead     Capability = "config:read"
	CapabilityConfigWrite    Capability = "config:write"
	CapabilityRawVideoRead   Capability = "raw_video:read"
	CapabilityAlertsRead     Capability = "alerts:read"
	CapabilityAlertsWrite    Capability = "alerts:write"
	CapabilityEvidenceExport Capability = "evidence:export"
	CapabilityBiometricRead  Capability = "biometric:read"
	CapabilityAuditRead      Capability = "audit:read"
	CapabilityDataErase      Capability = "data:erase"
	CapabilityAnalyticsRead  Capability = "analytics:read"
	CapabilityEventsWrite    Capability = "events:write"
)

// ErrDenied intentionally does not reveal which authorization check failed.
var ErrDenied = errors.New("authorization denied")

// Request describes the verified resource scope being authorized.
type Request struct {
	Capability Capability
	TenantID   string
	SiteID     string
}

type roleGrant struct {
	capabilities map[Capability]struct{}
	tenantWide   bool
}

func grants(capabilities ...Capability) map[Capability]struct{} {
	result := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		result[capability] = struct{}{}
	}
	return result
}

var seedRoles = map[identity.Role]roleGrant{
	identity.RoleSuperAdmin: {
		tenantWide: true,
		capabilities: grants(
			CapabilityAuthRead, CapabilitySitesRead, CapabilityTenantManage,
			CapabilitySiteManage, CapabilityConfigRead, CapabilityConfigWrite,
			CapabilityRawVideoRead, CapabilityAlertsRead, CapabilityAlertsWrite,
			CapabilityEvidenceExport, CapabilityBiometricRead, CapabilityAuditRead,
			CapabilityAnalyticsRead,
			CapabilityEventsWrite,
		),
	},
	identity.RoleSiteAdmin: {
		capabilities: grants(
			CapabilityAuthRead, CapabilitySitesRead, CapabilitySiteManage,
			CapabilityConfigRead, CapabilityConfigWrite, CapabilityRawVideoRead,
			CapabilityAlertsRead, CapabilityAlertsWrite, CapabilityEvidenceExport,
			CapabilityBiometricRead, CapabilityAnalyticsRead,
			CapabilityEventsWrite,
		),
	},
	identity.RoleOperator: {
		capabilities: grants(
			CapabilityAuthRead, CapabilitySitesRead, CapabilityConfigRead,
			CapabilityRawVideoRead, CapabilityAlertsRead, CapabilityAlertsWrite,
			CapabilityEvidenceExport, CapabilityBiometricRead, CapabilityAnalyticsRead,
		),
	},
	identity.RoleAuditor: {
		capabilities: grants(
			CapabilityAuthRead, CapabilitySitesRead, CapabilityConfigRead,
			CapabilityEvidenceExport, CapabilityAuditRead, CapabilityAnalyticsRead,
		),
	},
	identity.RoleViewer: {
		capabilities: grants(
			CapabilityAuthRead, CapabilitySitesRead, CapabilityConfigRead,
			CapabilityAlertsRead, CapabilityAnalyticsRead,
		),
	},
}

var mfaCapabilities = map[Capability]struct{}{
	CapabilityEvidenceExport: {},
	CapabilityBiometricRead:  {},
	CapabilityDataErase:      {},
}

var dataClassCapabilities = map[Capability]string{
	CapabilityRawVideoRead:  "raw_video",
	CapabilityBiometricRead: "biometric",
}

// Authorize requires matching tenant, explicit token scope, a seed-role grant,
// MFA where required, and site containment for non-tenant-wide roles.
func Authorize(principal identity.Principal, request Request) error {
	if request.TenantID == "" || principal.TenantID != request.TenantID {
		return ErrDenied
	}
	if !principal.HasScope(string(request.Capability)) {
		return ErrDenied
	}
	if _, sensitive := mfaCapabilities[request.Capability]; sensitive && !principal.MFAAuthenticated() {
		return ErrDenied
	}
	if dataClass, sensitive := dataClassCapabilities[request.Capability]; sensitive && !principal.HasDataClass(dataClass) {
		return ErrDenied
	}

	for _, role := range principal.Roles {
		grant, known := seedRoles[role]
		if !known {
			continue
		}
		if (role == identity.RoleSuperAdmin || role == identity.RoleAuditor) && !principal.MFAAuthenticated() {
			continue
		}
		if _, allowed := grant.capabilities[request.Capability]; !allowed {
			continue
		}
		if request.SiteID == "" || grant.tenantWide || principal.HasSite(request.SiteID) {
			return nil
		}
	}
	return ErrDenied
}

// CanAccessSite applies tenant and site containment without requiring a second
// capability lookup after a list operation has already been authorized.
func CanAccessSite(principal identity.Principal, tenantID, siteID string) bool {
	if principal.TenantID != tenantID {
		return false
	}
	if principal.HasRole(identity.RoleSuperAdmin) {
		return principal.MFAAuthenticated()
	}
	return principal.HasSite(siteID)
}
