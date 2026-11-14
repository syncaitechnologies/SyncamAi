package siteingest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
)

// AlertLabels are the human names shown in a notification.
type AlertLabels struct {
	SiteName   string
	CameraName string
	ZoneName   string
	Timezone   string
}

// DescribeFunc resolves names for an alert. Missing names are left blank.
type DescribeFunc func(ctx context.Context, alert alerting.Alert) AlertLabels

// DefaultCooldown suppresses repeat notifications for the same zone and event
// type, so one person walking around a zone does not flood a phone.
const DefaultCooldown = 2 * time.Minute

// TelegramNotifier sends a photo (or text) message for each new alert to the
// Telegram chats linked to the alert's site.
type TelegramNotifier struct {
	Client   *TelegramClient
	Targets  TargetStore
	Evidence EvidenceStore
	Describe DescribeFunc
	Cooldown time.Duration
	Now      func() time.Time
	Logf     func(string, ...any)

	mu   sync.Mutex
	last map[string]time.Time
}

var severityRank = map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}

var alertTitles = map[string]string{
	"intrusion":               "🚨 Intrusion detected",
	"restricted_zone":         "🚨 Restricted zone entry",
	"loitering":               "⚠️ Person loitering",
	"vehicle_activity":        "🚗 Vehicle activity",
	"weapon_review":           "🔴 Possible weapon — please review now",
	"camera_health":           "📷 Camera problem",
	"fire_review":             "🔥 Possible fire",
	"smoke_review":            "🔥 Possible smoke",
	"abandoned_object_review": "🎒 Unattended object",
}

// Title returns the notification headline for an event type.
func Title(eventType string) string {
	if title, ok := alertTitles[eventType]; ok {
		return title
	}
	return "🔔 " + strings.ReplaceAll(eventType, "_", " ")
}

// Caption builds the plain-text notification body.
func Caption(alert alerting.Alert, labels AlertLabels) string {
	var builder strings.Builder
	builder.WriteString(Title(alert.EventType))
	if labels.ZoneName != "" {
		builder.WriteString(" — " + labels.ZoneName)
	}
	builder.WriteString("\n")
	if labels.SiteName != "" {
		builder.WriteString("Site: " + labels.SiteName + "\n")
	}
	if labels.CameraName != "" {
		builder.WriteString("Camera: " + labels.CameraName + "\n")
	}
	occurred := alert.OccurredAt
	zone := "UTC"
	if labels.Timezone != "" {
		if location, err := time.LoadLocation(labels.Timezone); err == nil {
			occurred = occurred.In(location)
			zone = labels.Timezone
		}
	}
	builder.WriteString("Time: " + occurred.Format("02 Jan 2006, 15:04:05") + " (" + zone + ")\n")
	if alert.EventType != "camera_health" {
		builder.WriteString(fmt.Sprintf("Confidence: %.0f%%\n", alert.Confidence*100))
	}
	builder.WriteString("Severity: " + strings.ToUpper(alert.Severity) + "\n")
	builder.WriteString("Open the SyncCam dashboard to review and acknowledge.")
	return builder.String()
}

func (n *TelegramNotifier) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

func (n *TelegramNotifier) logf(format string, args ...any) {
	if n.Logf != nil {
		n.Logf(format, args...)
	}
}

// allow applies the per-zone, per-type cooldown.
func (n *TelegramNotifier) allow(alert alerting.Alert) bool {
	cooldown := n.Cooldown
	if cooldown == 0 {
		cooldown = DefaultCooldown
	}
	key := alert.TenantID + "|" + alert.SiteID + "|" + alert.ZoneID + "|" + alert.EventType
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.last == nil {
		n.last = map[string]time.Time{}
	}
	now := n.now()
	if previous, ok := n.last[key]; ok && now.Sub(previous) < cooldown {
		return false
	}
	n.last[key] = now
	if len(n.last) > 10000 {
		for stored, at := range n.last {
			if now.Sub(at) > cooldown {
				delete(n.last, stored)
			}
		}
	}
	return true
}

// AlertCreated implements alerting.Notifier.
func (n *TelegramNotifier) AlertCreated(parent context.Context, alert alerting.Alert, evidenceRefs []string) {
	if n == nil || n.Client == nil || n.Targets == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	targets, err := n.Targets.Targets(ctx, alert.TenantID, alert.SiteID)
	if err != nil {
		n.logf("telegram targets unavailable: %v", err)
		return
	}
	recipients := make([]TelegramTarget, 0, len(targets))
	for _, target := range targets {
		if !target.Muted && severityRank[alert.Severity] >= severityRank[target.MinSeverity] {
			recipients = append(recipients, target)
		}
	}
	if len(recipients) == 0 || !n.allow(alert) {
		return
	}
	labels := AlertLabels{}
	if n.Describe != nil {
		labels = n.Describe(ctx, alert)
	}
	caption := Caption(alert, labels)
	var photo []byte
	if n.Evidence != nil {
		for _, ref := range evidenceRefs {
			if strings.HasPrefix(ref, SnapshotRefPrefix) {
				if data, err := n.Evidence.Get(ctx, alert.TenantID, ref); err == nil {
					photo = data
				} else {
					n.logf("alert snapshot unavailable: %v", err)
				}
				break
			}
		}
	}
	for _, target := range recipients {
		if photo != nil {
			err = n.Client.SendPhoto(ctx, target.ChatID, caption, photo)
		} else {
			err = n.Client.SendMessage(ctx, target.ChatID, caption)
		}
		if err != nil {
			n.logf("telegram delivery failed for target %s: %v", target.ID, err)
		}
	}
}

// MemoryTargetStore is a deterministic TargetStore for tests.
type MemoryTargetStore struct {
	mu      sync.Mutex
	targets []TelegramTarget
	codes   map[string]memoryLink
}

type memoryLink struct {
	tenantID, siteID string
	expiresAt        time.Time
	used             bool
}

func NewMemoryTargetStore(targets ...TelegramTarget) *MemoryTargetStore {
	return &MemoryTargetStore{targets: append([]TelegramTarget(nil), targets...), codes: map[string]memoryLink{}}
}

func (s *MemoryTargetStore) Targets(_ context.Context, tenantID, siteID string) ([]TelegramTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]TelegramTarget, 0)
	for _, target := range s.targets {
		if target.TenantID == tenantID && (siteID == "" || target.SiteID == siteID) {
			result = append(result, target)
		}
	}
	return result, nil
}

func (s *MemoryTargetStore) CreateLinkCode(_ context.Context, tenantID, siteID, _ string) (string, time.Time, error) {
	code, err := NewLinkCode()
	if err != nil {
		return "", time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expiresAt := time.Now().UTC().Add(LinkCodeTTL)
	s.codes[code] = memoryLink{tenantID: tenantID, siteID: siteID, expiresAt: expiresAt}
	return code, expiresAt, nil
}

func (s *MemoryTargetStore) Redeem(_ context.Context, code string, chatID int64, title string) (string, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.codes[normalizeLinkCode(code)]
	if !ok || link.used || time.Now().After(link.expiresAt) {
		return "", "", false, nil
	}
	link.used = true
	s.codes[normalizeLinkCode(code)] = link
	s.targets = append(s.targets, TelegramTarget{ID: fmt.Sprintf("target-%d", len(s.targets)+1), TenantID: link.tenantID, SiteID: link.siteID, ChatID: chatID, ChatTitle: title, MinSeverity: "medium", CreatedAt: time.Now().UTC()})
	return link.tenantID, link.siteID, true, nil
}

func (s *MemoryTargetStore) Delete(_ context.Context, tenantID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, target := range s.targets {
		if target.ID == targetID && target.TenantID == tenantID {
			s.targets = append(s.targets[:index], s.targets[index+1:]...)
			return nil
		}
	}
	return ErrKeyNotFound
}
