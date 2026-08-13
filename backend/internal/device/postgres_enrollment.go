package device

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

type PostgresEnrollmentRepository struct {
	pool   transactionPool
	tokens *ClaimTokenManager
}

func NewPostgresEnrollmentRepository(pool transactionPool, tokens *ClaimTokenManager) *PostgresEnrollmentRepository {
	return &PostgresEnrollmentRepository{pool: pool, tokens: tokens}
}

func (r *PostgresEnrollmentRepository) IssueClaim(ctx context.Context, command IssueClaimCommand) (IssueClaimResult, error) {
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return IssueClaimResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := command.TenantID + ":" + command.IdempotencyKey
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil {
		return IssueClaimResult{}, fmt.Errorf("lock device claim idempotency key: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM platform.idempotency_keys WHERE tenant_id = $1::uuid AND idempotency_key = $2 AND expires_at <= clock_timestamp()`, command.TenantID, command.IdempotencyKey); err != nil {
		return IssueClaimResult{}, fmt.Errorf("expire device claim idempotency key: %w", err)
	}
	requestHash, err := hashIssueClaim(command)
	if err != nil {
		return IssueClaimResult{}, err
	}
	var storedHash string
	var storedResponse []byte
	err = tx.QueryRow(ctx, `SELECT request_hash, response_body FROM platform.idempotency_keys WHERE tenant_id = $1::uuid AND idempotency_key = $2`, command.TenantID, command.IdempotencyKey).Scan(&storedHash, &storedResponse)
	if err == nil {
		if storedHash != requestHash {
			return IssueClaimResult{}, ErrIdempotencyConflict
		}
		var claim DeviceClaim
		if err := json.Unmarshal(storedResponse, &claim); err != nil {
			return IssueClaimResult{}, fmt.Errorf("decode idempotent device claim response: %w", err)
		}
		token, err := r.tokens.Token(claim.ID, claim.TenantID)
		if err != nil {
			return IssueClaimResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return IssueClaimResult{}, fmt.Errorf("commit device claim replay: %w", err)
		}
		return IssueClaimResult{Claim: claim, ClaimToken: token, Replayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IssueClaimResult{}, fmt.Errorf("read device claim idempotency key: %w", err)
	}
	var siteExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM config.sites WHERE id = $1::uuid AND tenant_id = $2::uuid AND status <> 'retired')`, command.SiteID, command.TenantID).Scan(&siteExists); err != nil {
		return IssueClaimResult{}, fmt.Errorf("validate device claim site: %w", err)
	}
	if !siteExists {
		return IssueClaimResult{}, ErrSiteNotFound
	}

	device := EdgeDevice{
		ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID,
		SerialNumber: normalizeSerial(command.SerialNumber), HardwareTier: normalizeHardwareTier(command.HardwareTier),
		Model: strings.TrimSpace(command.Model), Status: "pending", CertificateStatus: "pending",
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO config.edge_devices (id, tenant_id, site_id, serial_number, hardware_tier, model, created_by, updated_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, NULLIF($6, ''), $7, $7)
		RETURNING created_at, updated_at`,
		device.ID, device.TenantID, device.SiteID, device.SerialNumber, device.HardwareTier, device.Model, command.ActorID,
	).Scan(&device.CreatedAt, &device.UpdatedAt); err != nil {
		return IssueClaimResult{}, classifyEnrollmentWrite(err)
	}
	claim := DeviceClaim{
		ID: uuid.NewString(), DeviceID: device.ID, TenantID: device.TenantID, SiteID: device.SiteID,
		SerialNumber: device.SerialNumber, HardwareTier: device.HardwareTier, Model: device.Model,
	}
	token, err := r.tokens.Token(claim.ID, claim.TenantID)
	if err != nil {
		return IssueClaimResult{}, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO platform.device_claims (id, tenant_id, device_id, token_hash, created_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)
		RETURNING created_at, expires_at`,
		claim.ID, claim.TenantID, claim.DeviceID, claimTokenHash(token), command.ActorID,
	).Scan(&claim.CreatedAt, &claim.ExpiresAt); err != nil {
		return IssueClaimResult{}, classifyEnrollmentWrite(err)
	}
	response, err := json.Marshal(claim)
	if err != nil {
		return IssueClaimResult{}, fmt.Errorf("encode device claim response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.idempotency_keys (tenant_id, idempotency_key, request_hash, response_status, resource_type, resource_id, response_body)
		VALUES ($1::uuid, $2, $3, 201, 'device_claim', $4::uuid, $5::jsonb)`, command.TenantID, command.IdempotencyKey, requestHash, claim.ID, response); err != nil {
		return IssueClaimResult{}, fmt.Errorf("store idempotent device claim response: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID, Action: "device.claim_issued", ResourceType: "edge_device",
		ResourceID: device.ID, RequestID: command.RequestID, AfterState: claim, OccurredAt: claim.CreatedAt,
	}); err != nil {
		return IssueClaimResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueClaimResult{}, fmt.Errorf("commit device claim issuance: %w", err)
	}
	return IssueClaimResult{Claim: claim, ClaimToken: token}, nil
}

func (r *PostgresEnrollmentRepository) Activate(ctx context.Context, command ActivateDeviceCommand) (EdgeDevice, error) {
	token := strings.TrimSpace(command.ClaimToken)
	claimID, tenantID, err := r.tokens.Verify(token)
	if err != nil {
		return EdgeDevice{}, err
	}
	tx, err := r.begin(ctx, tenantID, pgx.ReadWrite)
	if err != nil {
		return EdgeDevice{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var device EdgeDevice
	var storedHash []byte
	var expiresAt, databaseNow time.Time
	var consumedAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		SELECT d.id::text, d.tenant_id::text, d.site_id::text, d.serial_number, d.hardware_tier,
			COALESCE(d.model, ''), d.status, d.cert_status, d.activated_at, d.created_at, d.updated_at,
			c.token_hash, c.expires_at, c.consumed_at, clock_timestamp()
		FROM platform.device_claims c
		JOIN config.edge_devices d ON d.id = c.device_id AND d.tenant_id = c.tenant_id
		WHERE c.id = $1::uuid AND d.id = $2::uuid
		FOR UPDATE OF c, d`, claimID, command.DeviceID).Scan(
		&device.ID, &device.TenantID, &device.SiteID, &device.SerialNumber, &device.HardwareTier,
		&device.Model, &device.Status, &device.CertificateStatus, &device.ActivatedAt, &device.CreatedAt, &device.UpdatedAt,
		&storedHash, &expiresAt, &consumedAt, &databaseNow,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return EdgeDevice{}, ErrClaimInvalid
	}
	if err != nil {
		return EdgeDevice{}, fmt.Errorf("read device claim: %w", err)
	}
	if subtle.ConstantTimeCompare(storedHash, claimTokenHash(token)) != 1 || device.TenantID != tenantID {
		return EdgeDevice{}, ErrClaimInvalid
	}
	if consumedAt.Valid {
		return EdgeDevice{}, ErrClaimConsumed
	}
	if !databaseNow.Before(expiresAt) {
		return EdgeDevice{}, ErrClaimExpired
	}
	if normalizeSerial(command.SerialNumber) != device.SerialNumber {
		return EdgeDevice{}, ErrClaimSerialMismatch
	}
	var activatedAt time.Time
	if err := tx.QueryRow(ctx, `UPDATE platform.device_claims SET consumed_at = clock_timestamp() WHERE id = $1::uuid AND consumed_at IS NULL RETURNING consumed_at`, claimID).Scan(&activatedAt); err != nil {
		return EdgeDevice{}, fmt.Errorf("consume device claim: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		UPDATE config.edge_devices SET status = 'active', activated_at = $2, updated_at = $2, updated_by = $3
		WHERE id = $1::uuid RETURNING updated_at`, device.ID, activatedAt, "device:"+device.ID).Scan(&device.UpdatedAt); err != nil {
		return EdgeDevice{}, fmt.Errorf("activate edge device: %w", err)
	}
	device.Status = "active"
	device.ActivatedAt = &activatedAt
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: tenantID, ActorID: "device:" + device.ID, Action: "device.activated", ResourceType: "edge_device",
		ResourceID: device.ID, RequestID: command.RequestID, AfterState: device, OccurredAt: activatedAt,
	}); err != nil {
		return EdgeDevice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EdgeDevice{}, fmt.Errorf("commit device activation: %w", err)
	}
	return device, nil
}

func (r *PostgresEnrollmentRepository) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if r == nil || r.pool == nil || r.tokens == nil {
		return nil, fmt.Errorf("postgres enrollment repository is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid verified tenant identifier: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin enrollment transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set enrollment tenant context: %w", err)
	}
	return tx, nil
}

func classifyEnrollmentWrite(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrSiteNotFound
		case "23505":
			return ErrDeviceSerialConflict
		}
	}
	return fmt.Errorf("write device enrollment state: %w", err)
}
