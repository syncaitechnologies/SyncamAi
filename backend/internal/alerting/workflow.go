package alerting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
)

var (
	ErrAlertNotFound       = errors.New("alert not found")
	ErrAlertStateConflict  = errors.New("alert state conflict")
	ErrIdempotencyConflict = errors.New("alert idempotency conflict")
)

type AcknowledgeCommand struct {
	TenantID       string
	SiteID         string
	AlertID        string
	ActorID        string
	RequestID      string
	IdempotencyKey string
}

type AcknowledgeResult struct {
	Alert    Alert `json:"alert"`
	Replayed bool  `json:"replayed"`
}

func (r *PostgresRepository) Get(ctx context.Context, tenantID, alertID string) (Alert, error) {
	if r == nil || r.pool == nil {
		return Alert{}, errors.New("postgres alert repository is unavailable")
	}
	if _, err := uuid.Parse(alertID); err != nil {
		return Alert{}, ErrAlertNotFound
	}
	tx, err := beginTenantTx(ctx, r.pool, tenantID, pgx.ReadOnly)
	if err != nil {
		return Alert{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	alert, err := scanAlert(tx.QueryRow(ctx, alertSelect+` WHERE tenant_id = $1::uuid AND alert_id = $2::uuid`, tenantID, alertID))
	if err == pgx.ErrNoRows {
		return Alert{}, ErrAlertNotFound
	}
	if err != nil {
		return Alert{}, fmt.Errorf("get alert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Alert{}, fmt.Errorf("commit alert read: %w", err)
	}
	return alert, nil
}

func (r *PostgresRepository) Acknowledge(ctx context.Context, command AcknowledgeCommand) (AcknowledgeResult, error) {
	if r == nil || r.pool == nil {
		return AcknowledgeResult{}, errors.New("postgres alert repository is unavailable")
	}
	if err := validateAcknowledge(command); err != nil {
		return AcknowledgeResult{}, err
	}
	requestHash := acknowledgeHash(command.AlertID)
	tx, err := beginTenantTx(ctx, r.pool, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return AcknowledgeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", command.TenantID+":"+command.IdempotencyKey); err != nil {
		return AcknowledgeResult{}, fmt.Errorf("lock alert acknowledgment: %w", err)
	}
	var storedHash string
	var storedResponse []byte
	err = tx.QueryRow(ctx, `
		SELECT request_hash, response_body
		FROM platform.idempotency_keys
		WHERE tenant_id = $1::uuid AND idempotency_key = $2`,
		command.TenantID, command.IdempotencyKey).Scan(&storedHash, &storedResponse)
	if err == nil {
		if storedHash != requestHash {
			return AcknowledgeResult{}, ErrIdempotencyConflict
		}
		var alert Alert
		if err := json.Unmarshal(storedResponse, &alert); err != nil {
			return AcknowledgeResult{}, fmt.Errorf("decode alert acknowledgment replay: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return AcknowledgeResult{}, fmt.Errorf("commit alert acknowledgment replay: %w", err)
		}
		return AcknowledgeResult{Alert: alert, Replayed: true}, nil
	}
	if err != pgx.ErrNoRows {
		return AcknowledgeResult{}, fmt.Errorf("read alert acknowledgment replay: %w", err)
	}
	alert, err := scanAlert(tx.QueryRow(ctx, alertSelect+`
		WHERE tenant_id = $1::uuid AND alert_id = $2::uuid AND site_id = $3::uuid
		FOR UPDATE`, command.TenantID, command.AlertID, command.SiteID))
	if err == pgx.ErrNoRows {
		return AcknowledgeResult{}, ErrAlertNotFound
	}
	if err != nil {
		return AcknowledgeResult{}, fmt.Errorf("lock alert: %w", err)
	}
	if alert.Status != "unacknowledged" {
		return AcknowledgeResult{}, ErrAlertStateConflict
	}
	before := alert
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE alerts.alerts
		SET status = 'acknowledged', acked_at = $4, acked_by = $5, updated_at = $4
		WHERE tenant_id = $1::uuid AND alert_id = $2::uuid AND site_id = $3::uuid
			AND status = 'unacknowledged'`, command.TenantID, command.AlertID, command.SiteID, now, command.ActorID)
	if err != nil {
		return AcknowledgeResult{}, fmt.Errorf("acknowledge alert: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return AcknowledgeResult{}, ErrAlertStateConflict
	}
	alert.Status = "acknowledged"
	alert.AckedAt = &now
	alert.AckedBy = command.ActorID
	if _, err := tx.Exec(ctx, `
		INSERT INTO alerts.alert_actions (
			action_id, tenant_id, alert_id, action, actor_type, actor_id, request_id, payload
		) VALUES ($1::uuid, $2::uuid, $3::uuid, 'acknowledge', 'user', $4, $5::uuid, '{}'::jsonb)`,
		uuid.NewString(), command.TenantID, command.AlertID, command.ActorID, command.RequestID,
	); err != nil {
		return AcknowledgeResult{}, fmt.Errorf("record alert action: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID, Action: "alert.acknowledged",
		ResourceType: "alert", ResourceID: command.AlertID, RequestID: command.RequestID,
		BeforeState: before, AfterState: alert, OccurredAt: now,
	}); err != nil {
		return AcknowledgeResult{}, fmt.Errorf("audit alert acknowledgment: %w", err)
	}
	if _, err := realtime.Append(ctx, tx, realtime.Event{
		TenantID: command.TenantID, SiteID: command.SiteID,
		Topic: realtime.TopicAlertState, Payload: map[string]any{"alert": alert, "action": "acknowledge"},
	}); err != nil {
		return AcknowledgeResult{}, fmt.Errorf("publish alert acknowledgment: %w", err)
	}
	response, err := json.Marshal(alert)
	if err != nil {
		return AcknowledgeResult{}, fmt.Errorf("encode alert acknowledgment response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.idempotency_keys (
			tenant_id, idempotency_key, request_hash, response_status,
			resource_type, resource_id, response_body
		) VALUES ($1::uuid, $2, $3, 200, 'alert_acknowledgment', $4::uuid, $5::jsonb)`,
		command.TenantID, command.IdempotencyKey, requestHash, command.AlertID, response,
	); err != nil {
		return AcknowledgeResult{}, fmt.Errorf("store alert acknowledgment replay: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AcknowledgeResult{}, fmt.Errorf("commit alert acknowledgment: %w", err)
	}
	return AcknowledgeResult{Alert: alert}, nil
}

// DismissReasons are the reasons an operator can give for removing an alert
// from the queue. The reason is kept in the alert's action and audit records.
var DismissReasons = map[string]struct{}{
	"False positive":             {},
	"Duplicate event":            {},
	"Handled / no action needed": {},
}

// MaxDismissBatch is the most alerts one dismissal may remove (one queue page).
const MaxDismissBatch = 100

// DismissCommand removes up to MaxDismissBatch alerts in one request.
// CanAccessSite decides, per alert site, whether the caller may remove it;
// alerts it rejects are left untouched.
type DismissCommand struct {
	TenantID      string
	AlertIDs      []string
	ActorID       string
	RequestID     string
	Reason        string
	CanAccessSite func(siteID string) bool
}

// Dismiss removes alerts from the operator queue in one transaction. Rows are
// kept (status "dismissed"); each removed alert gets its own action record
// (alert_actions.request_id is unique per tenant, so each action gets a fresh
// ID and carries the batch request ID in its payload), the batch gets one
// audit event, and each site gets realtime updates. It returns the IDs that
// are now dismissed, including ones that already were.
func (r *PostgresRepository) Dismiss(ctx context.Context, command DismissCommand) ([]string, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres alert repository is unavailable")
	}
	alertIDs, err := validateDismiss(command)
	if err != nil {
		return nil, err
	}
	tx, err := beginTenantTx(ctx, r.pool, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, alertSelect+`
		WHERE tenant_id = $1::uuid AND alert_id = ANY($2::uuid[])
		ORDER BY alert_id
		FOR UPDATE`, command.TenantID, alertIDs)
	if err != nil {
		return nil, fmt.Errorf("lock alerts: %w", err)
	}
	dismissed := []string{}
	var changed []Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("read alert: %w", err)
		}
		if !command.CanAccessSite(alert.SiteID) {
			continue
		}
		dismissed = append(dismissed, alert.ID)
		if alert.Status != "dismissed" {
			alert.Status = "dismissed"
			changed = append(changed, alert)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	if len(changed) == 0 {
		return dismissed, tx.Commit(ctx)
	}
	changedIDs := make([]string, len(changed))
	for index, alert := range changed {
		changedIDs[index] = alert.ID
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE alerts.alerts SET status = 'dismissed', updated_at = $3
		WHERE tenant_id = $1::uuid AND alert_id = ANY($2::uuid[])`,
		command.TenantID, changedIDs, now); err != nil {
		return nil, fmt.Errorf("dismiss alerts: %w", err)
	}
	payload, err := json.Marshal(map[string]string{"reason": command.Reason, "request_id": command.RequestID})
	if err != nil {
		return nil, fmt.Errorf("encode alert dismissal: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO alerts.alert_actions (
			action_id, tenant_id, alert_id, action, actor_type, actor_id, request_id, payload
		)
		SELECT gen_random_uuid(), $1::uuid, alert_id, 'dismiss', 'user', $2, gen_random_uuid(), $3::jsonb
		FROM unnest($4::uuid[]) AS alert_id`,
		command.TenantID, command.ActorID, payload, changedIDs,
	); err != nil {
		return nil, fmt.Errorf("record alert actions: %w", err)
	}
	resourceType, resourceID := "alert", changedIDs[0]
	if len(changedIDs) > 1 {
		resourceType, resourceID = "alert_batch", command.RequestID
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID, Action: "alert.dismissed",
		ResourceType: resourceType, ResourceID: resourceID, RequestID: command.RequestID,
		AfterState: map[string]any{"alert_ids": changedIDs, "status": "dismissed", "reason": command.Reason}, OccurredAt: now,
	}); err != nil {
		return nil, fmt.Errorf("audit alert dismissal: %w", err)
	}
	bySite := map[string][]any{}
	var sites []string
	for _, alert := range changed {
		if _, seen := bySite[alert.SiteID]; !seen {
			sites = append(sites, alert.SiteID)
		}
		bySite[alert.SiteID] = append(bySite[alert.SiteID], map[string]any{"alert": alert, "action": "dismiss"})
	}
	for _, site := range sites {
		if err := realtime.AppendMany(ctx, tx, command.TenantID, site, realtime.TopicAlertState, bySite[site]); err != nil {
			return nil, fmt.Errorf("publish alert dismissal: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit alert dismissal: %w", err)
	}
	return dismissed, nil
}

// validateDismiss checks the command and returns its alert IDs, de-duplicated.
// IDs that are not UUIDs can never match an alert, so they are dropped.
func validateDismiss(command DismissCommand) ([]string, error) {
	for _, identifier := range []string{command.TenantID, command.RequestID} {
		if _, err := uuid.Parse(identifier); err != nil {
			return nil, fmt.Errorf("invalid alert dismissal identifier: %w", err)
		}
	}
	if strings.TrimSpace(command.ActorID) == "" || len(command.ActorID) > 128 {
		return nil, errors.New("alert dismissal actor is required")
	}
	if _, ok := DismissReasons[command.Reason]; !ok {
		return nil, errors.New("alert dismissal reason is not recognised")
	}
	if command.CanAccessSite == nil {
		return nil, errors.New("alert dismissal needs a site access check")
	}
	if len(command.AlertIDs) == 0 || len(command.AlertIDs) > MaxDismissBatch {
		return nil, fmt.Errorf("dismiss between 1 and %d alerts", MaxDismissBatch)
	}
	seen := map[string]struct{}{}
	alertIDs := make([]string, 0, len(command.AlertIDs))
	for _, raw := range command.AlertIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		id := parsed.String()
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			alertIDs = append(alertIDs, id)
		}
	}
	return alertIDs, nil
}

const alertSelect = `
	SELECT alert_id::text, tenant_id::text, event_id::text, site_id::text,
		camera_id::text, zone_id::text, event_type, severity, status,
		confidence, occurred_at, created_at, acked_at, COALESCE(acked_by, '')
	FROM alerts.alerts`

type rowScanner interface{ Scan(...any) error }

func scanAlert(row rowScanner) (Alert, error) {
	var alert Alert
	var ackedAt pgtype.Timestamptz
	err := row.Scan(&alert.ID, &alert.TenantID, &alert.EventID, &alert.SiteID,
		&alert.CameraID, &alert.ZoneID, &alert.EventType, &alert.Severity,
		&alert.Status, &alert.Confidence, &alert.OccurredAt, &alert.CreatedAt,
		&ackedAt, &alert.AckedBy)
	if err == nil && ackedAt.Valid {
		value := ackedAt.Time
		alert.AckedAt = &value
	}
	return alert, err
}

func validateAcknowledge(command AcknowledgeCommand) error {
	for _, identifier := range []string{command.TenantID, command.SiteID, command.AlertID, command.RequestID} {
		if _, err := uuid.Parse(identifier); err != nil {
			return fmt.Errorf("invalid alert acknowledgment identifier: %w", err)
		}
	}
	if strings.TrimSpace(command.ActorID) == "" || len(command.ActorID) > 128 {
		return errors.New("alert acknowledgment actor is required")
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 128 {
		return errors.New("alert acknowledgment idempotency key is required")
	}
	return nil
}

func acknowledgeHash(alertID string) string {
	sum := sha256.Sum256([]byte("acknowledge:" + alertID))
	return hex.EncodeToString(sum[:])
}

var memoryLocks sync.Map

func (r *MemoryRepository) Get(_ context.Context, tenantID, alertID string) (Alert, error) {
	if r == nil {
		return Alert{}, ErrAlertNotFound
	}
	for _, alert := range r.Alerts {
		if alert.TenantID == tenantID && alert.ID == alertID {
			return alert, nil
		}
	}
	return Alert{}, ErrAlertNotFound
}

func (r *MemoryRepository) Acknowledge(_ context.Context, command AcknowledgeCommand) (AcknowledgeResult, error) {
	if err := validateAcknowledge(command); err != nil {
		return AcknowledgeResult{}, err
	}
	lockValue, _ := memoryLocks.LoadOrStore(r, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	for index := range r.Alerts {
		alert := &r.Alerts[index]
		if alert.TenantID != command.TenantID || alert.SiteID != command.SiteID || alert.ID != command.AlertID {
			continue
		}
		if alert.Status != "unacknowledged" {
			return AcknowledgeResult{}, ErrAlertStateConflict
		}
		now := time.Now().UTC()
		alert.Status, alert.AckedAt, alert.AckedBy = "acknowledged", &now, command.ActorID
		return AcknowledgeResult{Alert: *alert}, nil
	}
	return AcknowledgeResult{}, ErrAlertNotFound
}

func (r *MemoryRepository) Dismiss(_ context.Context, command DismissCommand) ([]string, error) {
	alertIDs, err := validateDismiss(command)
	if err != nil {
		return nil, err
	}
	lockValue, _ := memoryLocks.LoadOrStore(r, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	wanted := map[string]struct{}{}
	for _, id := range alertIDs {
		wanted[id] = struct{}{}
	}
	dismissed := []string{}
	for index := range r.Alerts {
		alert := &r.Alerts[index]
		if _, ok := wanted[alert.ID]; ok && alert.TenantID == command.TenantID && command.CanAccessSite(alert.SiteID) {
			alert.Status = "dismissed"
			dismissed = append(dismissed, alert.ID)
		}
	}
	return dismissed, nil
}
