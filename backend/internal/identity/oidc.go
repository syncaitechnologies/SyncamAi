package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
)

// Verifier converts a signed bearer token into the provider-neutral principal.
type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

// Identity is a verified signed-in user who may not have a workspace yet. It
// carries no authorization and is used only to create a first workspace
// (ADR-016).
type Identity struct {
	UserID        string
	Email         string
	MFALevel      string
	EmailVerified bool
	// HasWorkspace is true when the token already carries workspace claims.
	HasWorkspace bool
}

// IdentityVerifier verifies a token without requiring workspace claims.
type IdentityVerifier interface {
	VerifyIdentity(context.Context, string) (Identity, error)
}

// OIDCVerifier validates tokens through OIDC discovery and a cached JWKS.
type OIDCVerifier struct {
	verifier         *oidc.IDTokenVerifier
	expectedAudience string
	profile          OIDCProfile
}

// OIDCProfile controls how verified provider-specific claims are normalized.
// Generic preserves the existing OIDC/Cognito-compatible contract. Supabase
// reads authorization only from trusted app_metadata.syncam.
type OIDCProfile string

const (
	OIDCProfileGeneric  OIDCProfile = "generic"
	OIDCProfileSupabase OIDCProfile = "supabase"
)

func parseOIDCProfile(profile string) (OIDCProfile, error) {
	switch OIDCProfile(strings.ToLower(strings.TrimSpace(profile))) {
	case "", OIDCProfileGeneric:
		return OIDCProfileGeneric, nil
	case OIDCProfileSupabase:
		return OIDCProfileSupabase, nil
	default:
		return "", fmt.Errorf("identity: unsupported OIDC profile %q", profile)
	}
}

// NewOIDCVerifier performs strict discovery for the configured issuer.
func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	return NewOIDCVerifierForProfile(ctx, OIDCProfileGeneric, issuer, audience)
}

// NewOIDCVerifierForProfile performs strict discovery for the configured
// issuer and explicitly selects the provider claim profile.
func NewOIDCVerifierForProfile(ctx context.Context, profile OIDCProfile, issuer, audience string) (*OIDCVerifier, error) {
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" {
		return nil, errors.New("identity: issuer and audience are required")
	}
	normalizedProfile, err := parseOIDCProfile(string(profile))
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("identity: discover OIDC provider: %w", err)
	}
	return &OIDCVerifier{
		verifier:         provider.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		expectedAudience: strings.TrimSpace(audience),
		profile:          normalizedProfile,
	}, nil
}

// NewOIDCVerifierWithKeySet supports deterministic tests and providers whose
// discovery endpoint is managed separately. Issuer and audience remain strict.
func NewOIDCVerifierWithKeySet(issuer, audience string, keys oidc.KeySet) (*OIDCVerifier, error) {
	return NewOIDCVerifierWithKeySetForProfile(OIDCProfileGeneric, issuer, audience, keys)
}

// NewOIDCVerifierWithKeySetForProfile supports deterministic profile tests.
func NewOIDCVerifierWithKeySetForProfile(profile OIDCProfile, issuer, audience string, keys oidc.KeySet) (*OIDCVerifier, error) {
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" || keys == nil {
		return nil, errors.New("identity: issuer, audience, and key set are required")
	}
	normalizedProfile, err := parseOIDCProfile(string(profile))
	if err != nil {
		return nil, err
	}
	return &OIDCVerifier{
		verifier:         oidc.NewVerifier(issuer, keys, &oidc.Config{SkipClientIDCheck: true}),
		expectedAudience: strings.TrimSpace(audience),
		profile:          normalizedProfile,
	}, nil
}

type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		*s = values
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("identity: expected a string or string array claim")
	}
	*s = strings.Fields(value)
	return nil
}

type tokenClaims struct {
	Subject     string     `json:"sub"`
	Audience    stringList `json:"aud"`
	ClientID    string     `json:"client_id"`
	Email       string     `json:"email"`
	TenantID    string     `json:"tenant_id"`
	SiteIDs     stringList `json:"site_ids"`
	Scopes      stringList `json:"scopes"`
	OAuthScopes stringList `json:"scope"`
	Roles       stringList `json:"roles"`
	Groups      stringList `json:"cognito:groups"`
	DataClasses stringList `json:"data_class"`
	MFALevel    string     `json:"mfa_level"`
	TokenUse    string     `json:"token_use"`
	Role        string     `json:"role"`
	AAL         string     `json:"aal"`
	Anonymous   bool       `json:"is_anonymous"`
	AppMetadata struct {
		Syncam *syncamClaims `json:"syncam"`
		// Registration is set by the access-token hook only for users without
		// a membership (ADR-016).
		Registration *struct {
			EmailVerified bool `json:"email_verified"`
		} `json:"syncam_registration"`
		// Operator is set by the access-token hook for people who run the
		// SyncCam platform (ADR-018).
		Operator bool `json:"syncam_operator"`
	} `json:"app_metadata"`
}

type syncamClaims struct {
	TenantID    string     `json:"tenant_id"`
	SiteIDs     stringList `json:"site_ids"`
	Scopes      stringList `json:"scopes"`
	Roles       stringList `json:"roles"`
	DataClasses stringList `json:"data_class"`
	Plan        string     `json:"plan"`
	TrialEndsAt *string    `json:"trial_ends_at"`
}

func (v *OIDCVerifier) verifiedClaims(ctx context.Context, rawToken string) (tokenClaims, error) {
	if v == nil || v.verifier == nil {
		return tokenClaims{}, errors.New("identity: verifier is not configured")
	}
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return tokenClaims{}, fmt.Errorf("identity: verify token: %w", err)
	}
	var claims tokenClaims
	if err := token.Claims(&claims); err != nil {
		return tokenClaims{}, fmt.Errorf("identity: decode claims: %w", err)
	}
	if !claims.matchesAudience(v.expectedAudience) {
		return tokenClaims{}, errors.New("identity: token audience does not match the configured client")
	}
	return claims, nil
}

// VerifyIdentity validates a Supabase user token that may not carry workspace
// claims yet. It grants no authorization by itself.
func (v *OIDCVerifier) VerifyIdentity(ctx context.Context, rawToken string) (Identity, error) {
	claims, err := v.verifiedClaims(ctx, rawToken)
	if err != nil {
		return Identity{}, err
	}
	if v.profile != OIDCProfileSupabase {
		return Identity{}, errors.New("identity: workspace registration requires the Supabase profile")
	}
	if claims.Role != "authenticated" || claims.Anonymous {
		return Identity{}, errors.New("identity: Supabase token is not an authenticated user token")
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return Identity{}, errors.New("identity: subject must be a UUID")
	}
	result := Identity{
		UserID: claims.Subject, Email: claims.Email, MFALevel: claims.AAL,
		HasWorkspace: claims.AppMetadata.Syncam != nil,
	}
	if claims.AppMetadata.Registration != nil {
		result.EmailVerified = claims.AppMetadata.Registration.EmailVerified
	}
	return result, nil
}

// Verify validates the token before exposing any authorization claims.
func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (Principal, error) {
	claims, err := v.verifiedClaims(ctx, rawToken)
	if err != nil {
		return Principal{}, err
	}
	if v.profile == OIDCProfileSupabase {
		return principalFromSupabaseClaims(claims)
	}
	if claims.TokenUse != "access" {
		return Principal{}, errors.New("identity: bearer token is not an access token")
	}
	return principalFromGenericClaims(claims)
}

func principalFromGenericClaims(claims tokenClaims) (Principal, error) {
	if _, err := uuid.Parse(claims.TenantID); err != nil {
		return Principal{}, errors.New("identity: tenant claim must be a UUID")
	}
	for _, siteID := range claims.SiteIDs {
		if _, err := uuid.Parse(siteID); err != nil {
			return Principal{}, errors.New("identity: site claims must be UUIDs")
		}
	}
	if len(claims.Scopes) == 0 {
		claims.Scopes = claims.OAuthScopes
	}
	if len(claims.Roles) == 0 {
		claims.Roles = claims.Groups
	}

	principal := Principal{
		UserID:      claims.Subject,
		Email:       claims.Email,
		TenantID:    claims.TenantID,
		SiteIDs:     []string(claims.SiteIDs),
		Roles:       normalizeRoles(claims.Roles),
		Scopes:      []string(claims.Scopes),
		DataClasses: []string(claims.DataClasses),
		MFALevel:    claims.MFALevel,
	}
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func principalFromSupabaseClaims(claims tokenClaims) (Principal, error) {
	if claims.Role != "authenticated" || claims.Anonymous {
		return Principal{}, errors.New("identity: Supabase token is not an authenticated user token")
	}
	if claims.AppMetadata.Syncam == nil {
		return Principal{}, errors.New("identity: Supabase token is missing app_metadata.syncam")
	}
	authorization := claims.AppMetadata.Syncam
	if _, err := uuid.Parse(authorization.TenantID); err != nil {
		return Principal{}, errors.New("identity: Supabase tenant claim must be a UUID")
	}
	for _, siteID := range authorization.SiteIDs {
		if _, err := uuid.Parse(siteID); err != nil {
			return Principal{}, errors.New("identity: Supabase site claims must be UUIDs")
		}
	}
	switch authorization.Plan {
	case "", PlanTrial, PlanActive, PlanSuspended:
	default:
		return Principal{}, errors.New("identity: Supabase plan claim is invalid")
	}
	var trialEndsAt *time.Time
	if authorization.TrialEndsAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, *authorization.TrialEndsAt)
		if err != nil {
			return Principal{}, errors.New("identity: Supabase trial end claim must be a timestamp")
		}
		trialEndsAt = &parsed
	}
	principal := Principal{
		UserID:      claims.Subject,
		Email:       claims.Email,
		TenantID:    authorization.TenantID,
		SiteIDs:     []string(authorization.SiteIDs),
		Roles:       normalizeRoles(authorization.Roles),
		Scopes:      []string(authorization.Scopes),
		DataClasses: []string(authorization.DataClasses),
		MFALevel:    claims.AAL,
		Plan:        authorization.Plan,
		TrialEndsAt: trialEndsAt,
		Operator:    claims.AppMetadata.Operator,
	}
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func normalizeRoles(claims stringList) []Role {
	roles := make([]Role, 0, len(claims))
	for _, role := range claims {
		roles = append(roles, Role(role))
	}
	return roles
}

func (c tokenClaims) matchesAudience(expected string) bool {
	if expected == "" {
		return false
	}
	if c.ClientID == expected {
		return true
	}
	for _, audience := range c.Audience {
		if audience == expected {
			return true
		}
	}
	return false
}
