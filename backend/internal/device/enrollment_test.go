package device

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func testClaimTokens(t *testing.T) *ClaimTokenManager {
	t.Helper()
	tokens, err := NewClaimTokenManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func TestClaimTokenManagerSignsVerifiesAndRejectsTampering(t *testing.T) {
	tokens := testClaimTokens(t)
	claimID := "55555555-5555-4555-8555-555555555555"
	token, err := tokens.Token(claimID, tenantA)
	if err != nil {
		t.Fatal(err)
	}
	gotClaim, gotTenant, err := tokens.Verify(token)
	if err != nil || gotClaim != claimID || gotTenant != tenantA {
		t.Fatalf("unexpected verified token: %s %s %v", gotClaim, gotTenant, err)
	}
	if second, _ := tokens.Token(claimID, tenantA); second != token {
		t.Fatal("claim token must be deterministic for safe idempotent replay")
	}
	tampered := token[:len(token)-1] + "A"
	if _, _, err := tokens.Verify(tampered); !errors.Is(err, ErrClaimInvalid) {
		t.Fatalf("expected tampered token rejection, got %v", err)
	}
	if _, err := NewClaimTokenManager([]byte("short")); !errors.Is(err, ErrClaimTokenConfig) {
		t.Fatalf("expected weak key rejection, got %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	if _, err := NewClaimTokenManagerFromBase64(encoded); err != nil {
		t.Fatalf("expected valid base64url key: %v", err)
	}
	if _, err := NewClaimTokenManagerFromBase64("not valid!"); !errors.Is(err, ErrClaimTokenConfig) {
		t.Fatalf("expected invalid base64 rejection, got %v", err)
	}
	if _, err := tokens.Token("bad", tenantA); !errors.Is(err, ErrClaimInvalid) {
		t.Fatalf("expected invalid claim identifier rejection, got %v", err)
	}
	if _, err := tokens.Token(claimID, "bad"); !errors.Is(err, ErrClaimInvalid) {
		t.Fatalf("expected invalid tenant identifier rejection, got %v", err)
	}
	if _, _, err := (*ClaimTokenManager)(nil).Verify(token); !errors.Is(err, ErrClaimTokenConfig) {
		t.Fatalf("expected nil token manager rejection, got %v", err)
	}
	for _, malformed := range []string{"", "v1.only-two", "v2.payload.signature", "v1.bad.bad"} {
		if _, _, err := tokens.Verify(malformed); !errors.Is(err, ErrClaimInvalid) {
			t.Fatalf("expected malformed token rejection for %q, got %v", malformed, err)
		}
	}
}

func TestMemoryEnrollmentFailsClosedWithoutTokenManager(t *testing.T) {
	repository := NewMemoryEnrollmentRepository(nil)
	if _, err := repository.IssueClaim(context.Background(), IssueClaimCommand{}); !errors.Is(err, ErrClaimTokenConfig) {
		t.Fatalf("expected issuance configuration failure, got %v", err)
	}
}

func TestMemoryEnrollmentIssuesReplaysAndActivatesOnce(t *testing.T) {
	repository := NewMemoryEnrollmentRepository(testClaimTokens(t))
	fixed := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return fixed }
	command := IssueClaimCommand{
		TenantID: tenantA, ActorID: "user-1", RequestID: "request-1", IdempotencyKey: "claim-1",
		SiteID: siteA, SerialNumber: " edge-001 ", HardwareTier: " M ", Model: " Jetson Orin ",
	}
	issued, err := repository.IssueClaim(context.Background(), command)
	if err != nil || issued.Replayed || issued.Claim.SerialNumber != "EDGE-001" || issued.Claim.HardwareTier != "m" || issued.Claim.ExpiresAt.Sub(issued.Claim.CreatedAt) != claimLifetime || issued.ClaimToken == "" {
		t.Fatalf("unexpected claim: %+v %v", issued, err)
	}
	if strings.Contains(string(repository.claims[issued.Claim.ID].tokenHash), issued.ClaimToken) {
		t.Fatal("plaintext token leaked into stored claim state")
	}
	replayed, err := repository.IssueClaim(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.ClaimToken != issued.ClaimToken || replayed.Claim.ID != issued.Claim.ID {
		t.Fatalf("unexpected replay: %+v %v", replayed, err)
	}
	command.Model = "Different"
	if _, err := repository.IssueClaim(context.Background(), command); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	command.IdempotencyKey = "claim-2"
	if _, err := repository.IssueClaim(context.Background(), command); !errors.Is(err, ErrDeviceSerialConflict) {
		t.Fatalf("expected serial conflict, got %v", err)
	}

	repository.now = func() time.Time { return fixed.Add(time.Minute) }
	if _, err := repository.Activate(context.Background(), ActivateDeviceCommand{DeviceID: issued.Claim.DeviceID, ClaimToken: issued.ClaimToken, SerialNumber: "wrong"}); !errors.Is(err, ErrClaimSerialMismatch) {
		t.Fatalf("expected serial mismatch, got %v", err)
	}
	activated, err := repository.Activate(context.Background(), ActivateDeviceCommand{DeviceID: issued.Claim.DeviceID, ClaimToken: issued.ClaimToken, SerialNumber: "edge-001"})
	if err != nil || activated.Status != "active" || activated.TenantID != tenantA || activated.SiteID != siteA || activated.ActivatedAt == nil || activated.CertificateStatus != "pending" {
		t.Fatalf("unexpected activation: %+v %v", activated, err)
	}
	if _, err := repository.Activate(context.Background(), ActivateDeviceCommand{DeviceID: issued.Claim.DeviceID, ClaimToken: issued.ClaimToken, SerialNumber: "edge-001"}); !errors.Is(err, ErrClaimConsumed) {
		t.Fatalf("expected one-time token rejection, got %v", err)
	}
}

func TestMemoryEnrollmentRejectsExpiredAndForeignClaims(t *testing.T) {
	repository := NewMemoryEnrollmentRepository(testClaimTokens(t))
	fixed := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return fixed }
	issued, err := repository.IssueClaim(context.Background(), IssueClaimCommand{
		TenantID: tenantA, IdempotencyKey: "claim-expiry", SiteID: siteA, SerialNumber: "EDGE-EXPIRED", HardwareTier: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	repository.now = func() time.Time { return fixed.Add(claimLifetime) }
	if _, err := repository.Activate(context.Background(), ActivateDeviceCommand{DeviceID: issued.Claim.DeviceID, ClaimToken: issued.ClaimToken, SerialNumber: "EDGE-EXPIRED"}); !errors.Is(err, ErrClaimExpired) {
		t.Fatalf("expected exact-boundary expiry, got %v", err)
	}
	foreignTokens, _ := NewClaimTokenManager([]byte("abcdef0123456789abcdef0123456789"))
	foreign, _ := foreignTokens.Token(issued.Claim.ID, tenantA)
	if _, err := repository.Activate(context.Background(), ActivateDeviceCommand{DeviceID: issued.Claim.DeviceID, ClaimToken: foreign, SerialNumber: "EDGE-EXPIRED"}); !errors.Is(err, ErrClaimInvalid) {
		t.Fatalf("expected foreign signer rejection, got %v", err)
	}
}
