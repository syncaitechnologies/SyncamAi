package identity

import "testing"

func TestPrincipalValidate(t *testing.T) {
	valid := Principal{
		UserID: "user-1", TenantID: "tenant-a", SiteIDs: []string{"site-a"},
		Roles: []Role{RoleViewer}, Scopes: []string{"sites:read"},
		DataClasses: []string{"metadata"}, MFALevel: "password",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid principal rejected: %v", err)
	}

	tests := []Principal{
		{TenantID: "tenant-a", SiteIDs: []string{"site-a"}, Roles: []Role{RoleViewer}, Scopes: []string{"sites:read"}, DataClasses: []string{"metadata"}, MFALevel: "password"},
		{UserID: "user-1", SiteIDs: []string{"site-a"}, Roles: []Role{RoleViewer}, Scopes: []string{"sites:read"}, DataClasses: []string{"metadata"}, MFALevel: "password"},
		{UserID: "user-1", TenantID: "tenant-a", SiteIDs: []string{"site-a"}, Scopes: []string{"sites:read"}, DataClasses: []string{"metadata"}, MFALevel: "password"},
		{UserID: "user-1", TenantID: "tenant-a", SiteIDs: []string{"site-a"}, Roles: []Role{Role("unknown")}, Scopes: []string{"sites:read"}, DataClasses: []string{"metadata"}, MFALevel: "password"},
		{UserID: "user-1", TenantID: "tenant-a", SiteIDs: []string{"site-a"}, Roles: []Role{RoleViewer}, DataClasses: []string{"metadata"}, MFALevel: "password"},
		{UserID: "user-1", TenantID: "tenant-a", SiteIDs: []string{"site-a"}, Roles: []Role{RoleViewer}, Scopes: []string{"sites:read"}, MFALevel: "password"},
		{UserID: "user-1", TenantID: "tenant-a", SiteIDs: []string{"site-a"}, Roles: []Role{RoleViewer}, Scopes: []string{"sites:read"}, DataClasses: []string{"metadata"}},
		{UserID: "user-1", TenantID: "tenant-a", Roles: []Role{RoleViewer}, Scopes: []string{"sites:read"}, DataClasses: []string{"metadata"}, MFALevel: "password"},
	}
	for _, principal := range tests {
		if err := principal.Validate(); err == nil {
			t.Fatalf("invalid principal accepted: %+v", principal)
		}
	}
}

func TestPrincipalHelpers(t *testing.T) {
	principal := Principal{
		Roles:       []Role{RoleOperator},
		Scopes:      []string{"alerts:write"},
		SiteIDs:     []string{"site-a"},
		DataClasses: []string{"metadata"},
		MFALevel:    "t2",
	}
	if !principal.HasRole(RoleOperator) || principal.HasRole(RoleAuditor) {
		t.Fatal("role lookup returned the wrong result")
	}
	if !principal.HasScope("alerts:write") || principal.HasScope("audit:read") {
		t.Fatal("scope lookup returned the wrong result")
	}
	if !principal.HasSite("site-a") || principal.HasSite("site-b") {
		t.Fatal("site lookup returned the wrong result")
	}
	if !principal.HasDataClass("metadata") || principal.HasDataClass("biometric") {
		t.Fatal("data-class lookup returned the wrong result")
	}
	if !principal.MFAAuthenticated() {
		t.Fatal("expected t2 to count as MFA")
	}
	principal.MFALevel = "password"
	if principal.MFAAuthenticated() {
		t.Fatal("password-only authentication must not count as MFA")
	}
}
