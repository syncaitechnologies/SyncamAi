package usermanagement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
)

var ErrSupabaseInvitationProviderConfiguration = errors.New("supabase invitation provider configuration is invalid")

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// SupabaseInvitationProvider is a server-only adapter for GoTrue's invite
// endpoint. Construct it only in a separately deployed worker with a runtime
// secret; the web and Go HTTP service deliberately never construct this type.
type SupabaseInvitationProvider struct {
	inviteURL *url.URL
	secretKey string
	client    httpDoer
}

func NewSupabaseInvitationProvider(projectURL, secretKey string, client httpDoer) (*SupabaseInvitationProvider, error) {
	base, err := url.Parse(strings.TrimSpace(projectURL))
	if err != nil || base.Scheme != "https" || base.Host == "" || (base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" || strings.TrimSpace(secretKey) == "" {
		return nil, ErrSupabaseInvitationProviderConfiguration
	}
	if client == nil {
		client = http.DefaultClient
	}
	inviteURL := base.ResolveReference(&url.URL{Path: "/auth/v1/invite"})
	return &SupabaseInvitationProvider{inviteURL: inviteURL, secretKey: secretKey, client: client}, nil
}

func (p *SupabaseInvitationProvider) DeliverInvitation(ctx context.Context, request DeliveryRequest) error {
	if p == nil || p.inviteURL == nil || strings.TrimSpace(p.secretKey) == "" || p.client == nil {
		return ErrSupabaseInvitationProviderConfiguration
	}
	if request.Action != invitationDeliveryAction {
		return fmt.Errorf("unsupported Supabase lifecycle action %q", request.Action)
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		return errors.New("invitation delivery payload is invalid")
	}
	email := strings.TrimSpace(payload.Email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return errors.New("invitation delivery email is invalid")
	}
	body, err := json.Marshal(struct {
		Email string `json:"email"`
	}{Email: email})
	if err != nil {
		return errors.New("invitation delivery payload is invalid")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.inviteURL.String(), bytes.NewReader(body))
	if err != nil {
		return errors.New("invitation provider request is invalid")
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.secretKey)
	httpRequest.Header.Set("apikey", p.secretKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-Request-ID", request.ProviderOperationID)

	response, err := p.client.Do(httpRequest)
	if err != nil || response == nil {
		return supabaseReconciliationRequiredError{}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 2048))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return supabaseReconciliationRequiredError{}
	}
	return nil
}

// supabaseReconciliationRequiredError contains no provider body or secret. It
// makes the worker hold an ambiguous attempt instead of automatically retrying.
type supabaseReconciliationRequiredError struct{}

func (supabaseReconciliationRequiredError) Error() string { return "Supabase invitation result requires reconciliation" }
func (supabaseReconciliationRequiredError) ReconciliationReason() string {
	return "Supabase invitation result requires reconciliation"
}
