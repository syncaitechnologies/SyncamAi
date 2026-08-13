package realtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryTicketIsSingleUseAndClaimsAreBound(t *testing.T) {
	store := NewMemoryTicketStore()
	claims := TicketClaims{TenantID: testTenantID, SiteID: testSiteID, UserID: "operator-1"}
	ticket, expires, err := store.Issue(context.Background(), claims)
	if err != nil || ticket == "" || !expires.After(time.Now()) {
		t.Fatalf("ticket issue failed: %q %v %v", ticket, expires, err)
	}
	consumed, err := store.Consume(context.Background(), ticket)
	if err != nil || consumed != claims {
		t.Fatalf("ticket claims changed: %+v %v", consumed, err)
	}
	if _, err := store.Consume(context.Background(), ticket); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("ticket replay accepted: %v", err)
	}
}

func TestMemoryTicketExpiresAndRejectsIncompleteClaims(t *testing.T) {
	store := NewMemoryTicketStore()
	now := time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if _, _, err := store.Issue(context.Background(), TicketClaims{}); err == nil {
		t.Fatal("incomplete claims accepted")
	}
	ticket, _, err := store.Issue(context.Background(), TicketClaims{TenantID: testTenantID, SiteID: testSiteID, UserID: "operator-1"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(TicketTTL)
	if _, err := store.Consume(context.Background(), ticket); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("expired ticket accepted: %v", err)
	}
	if _, err := (*MemoryTicketStore)(nil).Consume(context.Background(), "ticket"); !errors.Is(err, ErrInvalidTicket) {
		t.Fatal("nil ticket store did not fail closed")
	}
}
