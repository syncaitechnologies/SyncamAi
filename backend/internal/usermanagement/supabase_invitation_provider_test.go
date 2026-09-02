package usermanagement

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestSupabaseInvitationProviderUsesServerOnlyInviteRequest(t *testing.T) {
	var captured *http.Request
	var body string
	provider, err := NewSupabaseInvitationProvider("https://example.supabase.co", "server-only-secret", httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		bytes, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		body = string(bytes)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	err = provider.DeliverInvitation(context.Background(), DeliveryRequest{Action: invitationDeliveryAction, Payload: []byte(`{"email":"invitee@example.test"}`), ProviderOperationID: "lifecycle:" + lifecycleRequestID})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Method != http.MethodPost || captured.URL.Path != "/auth/v1/invite" || captured.Header.Get("Authorization") != "Bearer server-only-secret" || captured.Header.Get("apikey") != "server-only-secret" || captured.Header.Get("X-Request-ID") != "lifecycle:"+lifecycleRequestID {
		t.Fatalf("unexpected Supabase request: %#v", captured)
	}
	if body != `{"email":"invitee@example.test"}` || strings.Contains(body, "metadata") {
		t.Fatalf("unexpected invitation body: %s", body)
	}
}

func TestSupabaseInvitationProviderFailsClosedOnInvalidOrAmbiguousDelivery(t *testing.T) {
	for _, input := range []string{"", "http://example.supabase.co", "https://example.supabase.co/path", "https://example.supabase.co?query=true"} {
		if _, err := NewSupabaseInvitationProvider(input, "secret", http.DefaultClient); !errors.Is(err, ErrSupabaseInvitationProviderConfiguration) {
			t.Fatalf("configuration %q error = %v", input, err)
		}
	}
	if _, err := NewSupabaseInvitationProvider("https://example.supabase.co", "", http.DefaultClient); !errors.Is(err, ErrSupabaseInvitationProviderConfiguration) {
		t.Fatalf("blank secret error = %v", err)
	}
	if provider, err := NewSupabaseInvitationProvider("https://example.supabase.co", "secret", nil); err != nil || provider.client == nil {
		t.Fatalf("default client = %#v, %v", provider, err)
	} else if client, ok := provider.client.(*http.Client); !ok || client.Timeout != 10*time.Second || client.CheckRedirect == nil {
		t.Fatalf("unsafe default invitation client = %#v", provider.client)
	} else if err := client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("default invitation client follows redirects: %v", err)
	}
	provider, err := NewSupabaseInvitationProvider("https://example.supabase.co", "secret", httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("provider details"))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.DeliverInvitation(context.Background(), DeliveryRequest{Action: invitationDeliveryAction, Payload: []byte(`{"email":"invitee@example.test"}`)}); err == nil {
		t.Fatal("ambiguous provider response must not succeed")
	} else {
		var reconciliation reconciliationRequiredError
		if !errors.As(err, &reconciliation) || reconciliation.ReconciliationReason() != "Supabase invitation result requires reconciliation" {
			t.Fatalf("unexpected ambiguous delivery error: %v", err)
		}
	}
	if err := provider.DeliverInvitation(context.Background(), DeliveryRequest{Action: invitationDeliveryAction, Payload: []byte(`{"email":"invalid"}`)}); err == nil {
		t.Fatal("invalid delivery email must fail")
	}
	if err := provider.DeliverInvitation(context.Background(), DeliveryRequest{Action: "disable", Payload: []byte(`{"email":"invitee@example.test"}`)}); err == nil {
		t.Fatal("unsupported delivery action must fail")
	}
	if err := provider.DeliverInvitation(context.Background(), DeliveryRequest{Action: invitationDeliveryAction, Payload: []byte(`not-json`)}); err == nil {
		t.Fatal("invalid delivery payload must fail")
	}
	var unavailable *SupabaseInvitationProvider
	if err := unavailable.DeliverInvitation(context.Background(), DeliveryRequest{}); !errors.Is(err, ErrSupabaseInvitationProviderConfiguration) {
		t.Fatalf("nil provider error = %v", err)
	}
}
