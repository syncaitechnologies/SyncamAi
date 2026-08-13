package realtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

const TicketTTL = 30 * time.Second

var ErrInvalidTicket = errors.New("invalid or expired realtime ticket")

type TicketClaims struct {
	TenantID string
	SiteID   string
	UserID   string
}

type TicketStore interface {
	Issue(context.Context, TicketClaims) (string, time.Time, error)
	Consume(context.Context, string) (TicketClaims, error)
}

type ticketEntry struct {
	claims  TicketClaims
	expires time.Time
}

type MemoryTicketStore struct {
	mu      sync.Mutex
	now     func() time.Time
	tickets map[[sha256.Size]byte]ticketEntry
}

func NewMemoryTicketStore() *MemoryTicketStore {
	return &MemoryTicketStore{now: time.Now, tickets: make(map[[sha256.Size]byte]ticketEntry)}
}

func (s *MemoryTicketStore) Issue(_ context.Context, claims TicketClaims) (string, time.Time, error) {
	if s == nil || claims.TenantID == "" || claims.SiteID == "" || claims.UserID == "" {
		return "", time.Time{}, errors.New("complete realtime ticket claims are required")
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.now == nil {
		s.now = time.Now
	}
	if s.tickets == nil {
		s.tickets = make(map[[sha256.Size]byte]ticketEntry)
	}
	now := s.now().UTC()
	for key, entry := range s.tickets {
		if !entry.expires.After(now) {
			delete(s.tickets, key)
		}
	}
	expires := now.Add(TicketTTL)
	s.tickets[hash] = ticketEntry{claims: claims, expires: expires}
	return token, expires, nil
}

func (s *MemoryTicketStore) Consume(_ context.Context, token string) (TicketClaims, error) {
	if s == nil || token == "" {
		return TicketClaims{}, ErrInvalidTicket
	}
	hash := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tickets[hash]
	delete(s.tickets, hash)
	if !ok || !entry.expires.After(s.now().UTC()) {
		return TicketClaims{}, ErrInvalidTicket
	}
	return entry.claims, nil
}
