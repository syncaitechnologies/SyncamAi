package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresStatusRepository struct{ pool transactionPool }

func NewPostgresStatusRepository(pool transactionPool) *PostgresStatusRepository {
	return &PostgresStatusRepository{pool: pool}
}

func (r *PostgresStatusRepository) ListDevices(ctx context.Context, tenantID, siteID string, observedAt time.Time) ([]EdgeDevice, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	query := edgeDeviceSelect + " WHERE status <> 'retired' ORDER BY id LIMIT 500"
	arguments := []any{}
	if siteID != "" {
		if _, err := uuid.Parse(siteID); err != nil {
			return nil, fmt.Errorf("invalid verified site identifier: %w", err)
		}
		query = edgeDeviceSelect + " WHERE site_id = $1::uuid AND status <> 'retired' ORDER BY id LIMIT 500"
		arguments = append(arguments, siteID)
	}
	rows, err := tx.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list edge devices: %w", err)
	}
	defer rows.Close()
	result := make([]EdgeDevice, 0)
	for rows.Next() {
		device, err := scanEdgeDevice(rows)
		if err != nil {
			return nil, err
		}
		device.Status = EffectiveDeviceStatus(device, observedAt)
		result = append(result, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge devices: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit edge device list: %w", err)
	}
	return result, nil
}

func (r *PostgresStatusRepository) RecordHeartbeat(ctx context.Context, command HeartbeatCommand) (HeartbeatResult, error) {
	if r == nil || r.pool == nil {
		return HeartbeatResult{}, fmt.Errorf("postgres device status repository is unavailable")
	}
	requestHash, err := hashHeartbeat(command)
	if err != nil {
		return HeartbeatResult{}, err
	}
	command.FirmwareVersion = strings.TrimSpace(command.FirmwareVersion)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return HeartbeatResult{}, fmt.Errorf("begin device heartbeat transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result HeartbeatResult
	var activatedAt, lastHeartbeat pgtype.Timestamptz
	err = tx.QueryRow(ctx, `
		SELECT device_id::text, tenant_id::text, site_id::text, serial_number, hardware_tier,
			COALESCE(model, ''), device_status, certificate_status, COALESCE(firmware_version, ''),
			store_forward_depth, uptime_seconds, last_heartbeat, activated_at, created_at, updated_at,
			observed_at, replayed
		FROM edge.record_device_heartbeat($1::uuid, $2::uuid, $3::char(64), $4, $5, $6, $7::varchar(128))`,
		command.DeviceID, command.HeartbeatID, requestHash, command.ReportedAt, command.UptimeSeconds,
		command.StoreForwardDepth, command.FirmwareVersion,
	).Scan(
		&result.Device.ID, &result.Device.TenantID, &result.Device.SiteID, &result.Device.SerialNumber,
		&result.Device.HardwareTier, &result.Device.Model, &result.Device.Status, &result.Device.CertificateStatus,
		&result.Device.FirmwareVersion, &result.Device.StoreForwardDepth, &result.Device.UptimeSeconds,
		&lastHeartbeat, &activatedAt, &result.Device.CreatedAt, &result.Device.UpdatedAt, &result.ObservedAt, &result.Replayed,
	)
	if err != nil {
		return HeartbeatResult{}, classifyHeartbeatError(err)
	}
	if lastHeartbeat.Valid {
		result.Device.LastHeartbeat = &lastHeartbeat.Time
	}
	if activatedAt.Valid {
		result.Device.ActivatedAt = &activatedAt.Time
	}
	if err := tx.Commit(ctx); err != nil {
		return HeartbeatResult{}, fmt.Errorf("commit device heartbeat: %w", err)
	}
	return result, nil
}

const edgeDeviceSelect = `
	SELECT id::text, tenant_id::text, site_id::text, serial_number, hardware_tier,
		COALESCE(model, ''), status, cert_status, COALESCE(firmware_version, ''),
		store_forward_depth, uptime_seconds, last_heartbeat, activated_at, created_at, updated_at
	FROM config.edge_devices`

func scanEdgeDevice(row rowScanner) (EdgeDevice, error) {
	var device EdgeDevice
	var activatedAt, lastHeartbeat pgtype.Timestamptz
	if err := row.Scan(
		&device.ID, &device.TenantID, &device.SiteID, &device.SerialNumber, &device.HardwareTier,
		&device.Model, &device.Status, &device.CertificateStatus, &device.FirmwareVersion,
		&device.StoreForwardDepth, &device.UptimeSeconds, &lastHeartbeat, &activatedAt,
		&device.CreatedAt, &device.UpdatedAt,
	); err != nil {
		return EdgeDevice{}, err
	}
	if lastHeartbeat.Valid {
		device.LastHeartbeat = &lastHeartbeat.Time
	}
	if activatedAt.Valid {
		device.ActivatedAt = &activatedAt.Time
	}
	return device, nil
}

func (r *PostgresStatusRepository) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("postgres device status repository is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid verified tenant identifier: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin device status transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set device status tenant context: %w", err)
	}
	return tx, nil
}

func classifyHeartbeatError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "28000":
			return ErrDeviceUnauthorized
		case "23505":
			return ErrHeartbeatConflict
		}
	}
	return fmt.Errorf("record device heartbeat: %w", err)
}
