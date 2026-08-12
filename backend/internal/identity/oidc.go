package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Verifier converts a signed bearer token into the provider-neutral principal.
type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

// OIDCVerifier validates tokens through OIDC discovery and a cached JWKS.
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewOIDCVerifier performs strict discovery for the configured issuer.
func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" {
		return nil, errors.New("identity: issuer and audience are required")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("identity: discover OIDC provider: %w", err)
	}
	return &OIDCVerifier{verifier: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

// NewOIDCVerifierWithKeySet supports deterministic tests and providers whose
// discovery endpoint is managed separately. Issuer and audience remain strict.
func NewOIDCVerifierWithKeySet(issuer, audience string, keys oidc.KeySet) (*OIDCVerifier, error) {
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" || keys == nil {
		return nil, errors.New("identity: issuer, audience, and key set are required")
	}
	return &OIDCVerifier{
		verifier: oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: audience}),
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
	Email       string     `json:"email"`
	TenantID    string     `json:"tenant_id"`
	SiteIDs     stringList `json:"site_ids"`
	Scopes      stringList `json:"scopes"`
	Roles       stringList `json:"roles"`
	DataClasses stringList `json:"data_class"`
	MFALevel    string     `json:"mfa_level"`
	TokenUse    string     `json:"token_use"`
}

// Verify validates the token before exposing any authorization claims.
func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (Principal, error) {
	if v == nil || v.verifier == nil {
		return Principal{}, errors.New("identity: verifier is not configured")
	}
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Principal{}, fmt.Errorf("identity: verify token: %w", err)
	}
	var claims tokenClaims
	if err := token.Claims(&claims); err != nil {
		return Principal{}, fmt.Errorf("identity: decode claims: %w", err)
	}
	if claims.TokenUse != "" && claims.TokenUse != "access" {
		return Principal{}, errors.New("identity: bearer token is not an access token")
	}

	roles := make([]Role, 0, len(claims.Roles))
	for _, role := range claims.Roles {
		roles = append(roles, Role(role))
	}
	principal := Principal{
		UserID:      claims.Subject,
		Email:       claims.Email,
		TenantID:    claims.TenantID,
		SiteIDs:     []string(claims.SiteIDs),
		Roles:       roles,
		Scopes:      []string(claims.Scopes),
		DataClasses: []string(claims.DataClasses),
		MFALevel:    claims.MFALevel,
	}
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}
