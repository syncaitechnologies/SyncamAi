package siteingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Site box offline alerts. A box sends a heartbeat every two minutes; when a
// box has been silent for OfflineAfter, its site's Telegram chats and the
// platform owner get one "offline" message, and one "back online" message when
// it reports again. Each change is returned exactly once by the database, so
// several checks never announce the same change twice.

// DefaultBoxOfflineAfter is five missed heartbeats.
const DefaultBoxOfflineAfter = 10 * time.Minute

// BoxTransition is one site box going offline or coming back online.
type BoxTransition struct {
	Kind         string // "offline" or "online"
	KeyID        string
	TenantID     string
	SiteID       string
	Workspace    string
	SiteName     string
	Timezone     string
	BoxLabel     string
	LastSeenAt   time.Time
	OfflineSince time.Time
}

// BoxPresenceStore detects and records presence changes.
type BoxPresenceStore interface {
	Transitions(ctx context.Context, offlineAfter time.Duration) ([]BoxTransition, error)
}

// TelegramSender sends a plain text message.
type TelegramSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// BoxWatchdog announces site boxes going offline and coming back.
type BoxWatchdog struct {
	Store   BoxPresenceStore
	Sender  TelegramSender
	Targets TargetStore
	// OwnerChatID, when not zero, also receives every workspace's notices.
	OwnerChatID  int64
	OfflineAfter time.Duration
	Logf         func(string, ...any)
}

func (w *BoxWatchdog) logf(format string, args ...any) {
	if w.Logf != nil {
		w.Logf(format, args...)
	}
}

func (w *BoxWatchdog) offlineAfter() time.Duration {
	if w.OfflineAfter <= 0 {
		return DefaultBoxOfflineAfter
	}
	return w.OfflineAfter
}

// Run checks every interval until ctx ends. It waits OfflineAfter before the
// first check so that boxes have time to report after the API restarts.
func (w *BoxWatchdog) Run(ctx context.Context, interval time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(w.offlineAfter()):
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check announces the changes found now.
func (w *BoxWatchdog) Check(ctx context.Context) {
	if w == nil || w.Store == nil {
		return
	}
	changes, err := w.Store.Transitions(ctx, w.offlineAfter())
	if err != nil {
		w.logf("site box presence check failed: %v", err)
		return
	}
	for _, change := range changes {
		w.announce(ctx, change)
	}
}

func (w *BoxWatchdog) announce(ctx context.Context, change BoxTransition) {
	if w.Sender == nil {
		return
	}
	text := BoxNotice(change, time.Now())
	chats := map[int64]struct{}{}
	if w.Targets != nil {
		targets, err := w.Targets.Targets(ctx, change.TenantID, change.SiteID)
		if err != nil {
			w.logf("telegram targets unavailable for a box notice: %v", err)
		}
		for _, target := range targets {
			if !target.Muted {
				chats[target.ChatID] = struct{}{}
			}
		}
	}
	if w.OwnerChatID != 0 {
		chats[w.OwnerChatID] = struct{}{}
	}
	for chat := range chats {
		if err := w.Sender.SendMessage(ctx, chat, text); err != nil {
			w.logf("box %s notice could not be sent to a chat: %v", change.Kind, err)
		}
	}
	w.logf("site box %s: %s / %s", change.Kind, change.Workspace, change.SiteName)
}

// BoxNotice is the Telegram text for a presence change, with times in the
// site's timezone.
func BoxNotice(change BoxTransition, now time.Time) string {
	location, err := time.LoadLocation(change.Timezone)
	if err != nil || change.Timezone == "" {
		location = time.UTC
	}
	where := change.SiteName
	if change.Workspace != "" {
		where += " (" + change.Workspace + ")"
	}
	box := strings.TrimSpace(change.BoxLabel)
	if box == "" {
		box = "Site box"
	}
	since := change.OfflineSince.In(location).Format("2 Jan 15:04 MST")
	if change.Kind == "online" {
		return fmt.Sprintf("🟢 Site box back online\n%s\n%s is reporting again after %s (silent since %s). If the computer stayed on but lost its internet connection, alerts from that time are being sent now; the box keeps them for up to 24 hours.",
			where, box, friendlyDuration(now.Sub(change.OfflineSince)), since)
	}
	return fmt.Sprintf("🔴 Site box offline\n%s\n%s has not reported since %s, so no alerts are coming from this site. Check that the computer is on, awake and connected to the internet.",
		where, box, since)
}

func friendlyDuration(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "about a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	case d < 48*time.Hour:
		hours, minutes := int(d/time.Hour), int(d%time.Hour/time.Minute)
		if minutes == 0 {
			return fmt.Sprintf("%d h", hours)
		}
		return fmt.Sprintf("%d h %d min", hours, minutes)
	default:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	}
}

// PostgresBoxPresence calls platform.site_box_transitions, which checks all
// workspaces' boxes and records each change once.
type PostgresBoxPresence struct{ pool transactionPool }

func NewPostgresBoxPresence(pool transactionPool) *PostgresBoxPresence {
	return &PostgresBoxPresence{pool: pool}
}

func (p *PostgresBoxPresence) Transitions(ctx context.Context, offlineAfter time.Duration) ([]BoxTransition, error) {
	if p == nil || p.pool == nil {
		return nil, errors.New("site box presence store is unavailable")
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, fmt.Errorf("begin site box presence check: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT kind, key_id::text, tenant_id::text, site_id::text, workspace, site_name, timezone, box_label, last_seen_at, offline_since
		FROM platform.site_box_transitions(make_interval(secs => $1))`, offlineAfter.Seconds())
	if err != nil {
		return nil, fmt.Errorf("check site box presence: %w", err)
	}
	changes := []BoxTransition{}
	for rows.Next() {
		var change BoxTransition
		if err := rows.Scan(&change.Kind, &change.KeyID, &change.TenantID, &change.SiteID, &change.Workspace, &change.SiteName,
			&change.Timezone, &change.BoxLabel, &change.LastSeenAt, &change.OfflineSince); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read site box presence: %w", err)
		}
		changes = append(changes, change)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read site box presence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit site box presence: %w", err)
	}
	return changes, nil
}
