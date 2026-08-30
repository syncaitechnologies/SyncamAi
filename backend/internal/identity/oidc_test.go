package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

func signedToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "test", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(encoded))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestOIDCVerifier(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewOIDCVerifierWithKeySet(
		"https://issuer.example.test",
		"syncam-web",
		&oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}},
	)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{
		"iss":        "https://issuer.example.test",
		"aud":        "syncam-web",
		"sub":        "user-1",
		"exp":        time.Now().Add(time.Minute).Unix(),
		"iat":        time.Now().Add(-time.Minute).Unix(),
		"tenant_id":  "11111111-1111-4111-8111-111111111111",
		"site_ids":   []string{"22222222-2222-4222-8222-222222222222"},
		"scopes":     "sites:read auth:read",
		"roles":      []string{"viewer"},
		"data_class": []string{"metadata"},
		"mfa_level":  "password",
		"token_use":  "access",
	}

	principal, err := verifier.Verify(context.Background(), signedToken(t, key, claims))
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if principal.UserID != "user-1" || principal.TenantID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if !principal.HasScope("sites:read") || !principal.HasRole(RoleViewer) {
		t.Fatalf("canonical claims were not normalized: %+v", principal)
	}

	claims["aud"] = "another-client"
	if _, err := verifier.Verify(context.Background(), signedToken(t, key, claims)); err == nil {
		t.Fatal("wrong audience token was accepted")
	}
	claims["aud"] = "syncam-web"
	claims["token_use"] = "id"
	if _, err := verifier.Verify(context.Background(), signedToken(t, key, claims)); err == nil || !strings.Contains(err.Error(), "access token") {
		t.Fatalf("ID token should be rejected, got %v", err)
	}
	claims["token_use"] = "access"
	delete(claims, "tenant_id")
	if _, err := verifier.Verify(context.Background(), signedToken(t, key, claims)); err == nil || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("missing tenant claim should be rejected, got %v", err)
	}
}

func TestOIDCVerifierConfiguration(t *testing.T) {
	if _, err := NewOIDCVerifierWithKeySet("", "audience", &oidc.StaticKeySet{}); err == nil {
		t.Fatal("missing issuer was accepted")
	}
	var verifier *OIDCVerifier
	if _, err := verifier.Verify(context.Background(), "token"); err == nil {
		t.Fatal("unconfigured verifier was accepted")
	}
}

func TestOIDCVerifierAcceptsCognitoAccessTokenClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewOIDCVerifierWithKeySet(
		"https://cognito-idp.ap-south-1.amazonaws.com/ap-south-1_example",
		"syncam-public-client",
		&oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}},
	)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{
		"iss":            "https://cognito-idp.ap-south-1.amazonaws.com/ap-south-1_example",
		"client_id":      "syncam-public-client",
		"sub":            "cognito-subject",
		"exp":            time.Now().Add(time.Minute).Unix(),
		"iat":            time.Now().Add(-time.Minute).Unix(),
		"tenant_id":      "11111111-1111-4111-8111-111111111111",
		"site_ids":       []string{"22222222-2222-4222-8222-222222222222"},
		"scope":          "sites:read auth:read",
		"cognito:groups": []string{"viewer"},
		"data_class":     []string{"metadata"},
		"mfa_level":      "password",
		"token_use":      "access",
	}
	principal, err := verifier.Verify(context.Background(), signedToken(t, key, claims))
	if err != nil {
		t.Fatalf("Cognito access token rejected: %v", err)
	}
	if !principal.HasScope("sites:read") || !principal.HasRole(RoleViewer) {
		t.Fatalf("Cognito claims were not normalized: %+v", principal)
	}
	claims["client_id"] = "different-client"
	if _, err := verifier.Verify(context.Background(), signedToken(t, key, claims)); err == nil {
		t.Fatal("wrong Cognito client_id was accepted")
	}
}

func TestOIDCVerifierAcceptsSupabaseAppMetadataClaimsOnly(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewOIDCVerifierWithKeySetForProfile(
		OIDCProfileSupabase,
		"https://frudzvjpvjpysxeneeeb.supabase.co/auth/v1",
		"authenticated",
		&oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}},
	)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{
		"iss":  "https://frudzvjpvjpysxeneeeb.supabase.co/auth/v1",
		"aud":  "authenticated",
		"sub":  "11111111-1111-4111-8111-111111111111",
		"role": "authenticated",
		"aal":  "aal2",
		"exp":  time.Now().Add(time.Minute).Unix(),
		"iat":  time.Now().Add(-time.Minute).Unix(),
		"app_metadata": map[string]any{"syncam": map[string]any{
			"tenant_id":  "22222222-2222-4222-8222-222222222222",
			"site_ids":   []string{"33333333-3333-4333-8333-333333333333"},
			"roles":      []string{"viewer"},
			"scopes":     []string{"sites:read"},
			"data_class": []string{"metadata"},
		}},
		// These top-level values simulate user-controlled or stale claims. The
		// Supabase profile must ignore them in favor of app_metadata.syncam.
		"tenant_id":  "44444444-4444-4444-8444-444444444444",
		"site_ids":   []string{"55555555-5555-4555-8555-555555555555"},
		"roles":      []string{"super_admin"},
		"scopes":     []string{"admin:*"},
		"data_class": []string{"biometric"},
	}

	principal, err := verifier.Verify(context.Background(), signedToken(t, key, claims))
	if err != nil {
		t.Fatalf("valid Supabase token rejected: %v", err)
	}
	if principal.TenantID != "22222222-2222-4222-8222-222222222222" || !principal.HasRole(RoleViewer) || principal.HasRole(RoleSuperAdmin) {
		t.Fatalf("Supabase claims were not normalized from app metadata: %+v", principal)
	}
	if !principal.MFAAuthenticated() {
		t.Fatalf("aal2 should satisfy the existing MFA boundary: %+v", principal)
	}

	delete(claims, "app_metadata")
	if _, err := verifier.Verify(context.Background(), signedToken(t, key, claims)); err == nil || !strings.Contains(err.Error(), "app_metadata") {
		t.Fatalf("missing trusted authorization claims should be rejected, got %v", err)
	}
	claims["app_metadata"] = map[string]any{"syncam": map[string]any{
		"tenant_id":  "22222222-2222-4222-8222-222222222222",
		"site_ids":   []string{"33333333-3333-4333-8333-333333333333"},
		"roles":      []string{"viewer"},
		"scopes":     []string{"sites:read"},
		"data_class": []string{"metadata"},
	}}
	claims["role"] = "anon"
	if _, err := verifier.Verify(context.Background(), signedToken(t, key, claims)); err == nil || !strings.Contains(err.Error(), "authenticated") {
		t.Fatalf("anonymous token should be rejected, got %v", err)
	}
}

func TestOIDCVerifierRejectsUnsupportedProfile(t *testing.T) {
	if _, err := NewOIDCVerifierWithKeySetForProfile("unknown", "https://issuer.example.test", "client", &oidc.StaticKeySet{}); err == nil {
		t.Fatal("unsupported OIDC profile was accepted")
	}
}
