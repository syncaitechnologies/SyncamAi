// Package identity defines the provider-neutral authenticated user contract.
package identity

import (
	"errors"
	"strings"
)

// Role is one of the canonical seed roles carried in a verified token.
type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleSiteAdmin  Role = "site_admin"
	RoleOperator   Role = "operator"
	RoleAuditor    Role = "auditor"
	RoleViewer     Role = "viewer"
)

// Principal contains only claims that passed signature, issuer, audience, and
// expiry verification. TenantID is never populated from a request header.
type Principal struct {
	UserID      string
	Email       string
	TenantID    string
	SiteIDs     []string
	Roles       []Role
	Scopes      []string
	DataClasses []string
	MFALevel    string
}

// Validate rejects tokens that cannot be safely used for authorization.
func (p Principal) Validate() error {
	if strings.TrimSpace(p.UserID) == "" {
		return errors.New("identity: missing subject")
	}
	if strings.TrimSpace(p.TenantID) == "" {
		return errors.New("identity: missing tenant claim")
	}
	if len(p.Roles) == 0 {
		return errors.New("identity: missing roles claim")
	}
	for _, role := range p.Roles {
		switch role {
		case RoleSuperAdmin, RoleSiteAdmin, RoleOperator, RoleAuditor, RoleViewer:
		default:
			return errors.New("identity: unknown seed role")
		}
	}
	if len(p.Scopes) == 0 {
		return errors.New("identity: missing scopes claim")
	}
	if len(p.DataClasses) == 0 {
		return errors.New("identity: missing data class claim")
	}
	if strings.TrimSpace(p.MFALevel) == "" {
		return errors.New("identity: missing MFA level claim")
	}
	if !p.HasRole(RoleSuperAdmin) && len(p.SiteIDs) == 0 {
		return errors.New("identity: site-scoped role has no sites")
	}
	return nil
}

// HasRole reports whether the principal was assigned the named role.
func (p Principal) HasRole(role Role) bool {
	for _, assigned := range p.Roles {
		if assigned == role {
			return true
		}
	}
	return false
}

// HasScope reports whether the verified token explicitly grants a capability.
func (p Principal) HasScope(scope string) bool {
	for _, granted := range p.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

// HasDataClass reports whether the token permits access to the named sensitive
// data class in addition to its role and capability grants.
func (p Principal) HasDataClass(dataClass string) bool {
	for _, granted := range p.DataClasses {
		if granted == dataClass {
			return true
		}
	}
	return false
}

// HasSite reports whether a site-scoped principal contains the requested site.
func (p Principal) HasSite(siteID string) bool {
	for _, granted := range p.SiteIDs {
		if granted == siteID {
			return true
		}
	}
	return false
}

// MFAAuthenticated reports whether the token records a completed MFA level.
func (p Principal) MFAAuthenticated() bool {
	level := strings.ToLower(strings.TrimSpace(p.MFALevel))
	return level != "" && level != "none" && level != "password"
}
