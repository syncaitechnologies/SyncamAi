package device

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

type transactionPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresRepository struct{ pool transactionPool }

func NewPostgresRepository(pool transactionPool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) List(ctx context.Context, tenantID, siteID string) ([]Camera, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := cameraSelect + " WHERE lifecycle_status <> 'retired' ORDER BY id LIMIT 500"
	arguments := []any{}
	if siteID != "" {
		if _, err := uuid.Parse(siteID); err != nil {
			return nil, fmt.Errorf("invalid verified site identifier: %w", err)
		}
		query = cameraSelect + " WHERE site_id = $1::uuid AND lifecycle_status <> 'retired' ORDER BY id LIMIT 500"
		arguments = append(arguments, siteID)
	}
	rows, err := tx.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list cameras: %w", err)
	}
	defer rows.Close()
	result := make([]Camera, 0)
	for rows.Next() {
		camera, err := scanCamera(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, camera)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cameras: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit camera list: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) Get(ctx context.Context, tenantID, cameraID string) (Camera, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly)
	if err != nil {
		return Camera{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	camera, err := scanCamera(tx.QueryRow(ctx, cameraSelect+" WHERE id = $1::uuid AND lifecycle_status <> 'retired'", cameraID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Camera{}, ErrCameraNotFound
	}
	if err != nil {
		return Camera{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Camera{}, fmt.Errorf("commit camera read: %w", err)
	}
	return camera, nil
}

func (r *PostgresRepository) Create(ctx context.Context, command CreateCameraCommand) (CreateCameraResult, error) {
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return CreateCameraResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := command.TenantID + ":" + command.IdempotencyKey
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil {
		return CreateCameraResult{}, fmt.Errorf("lock camera idempotency key: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM platform.idempotency_keys WHERE tenant_id = $1::uuid AND idempotency_key = $2 AND expires_at <= clock_timestamp()`, command.TenantID, command.IdempotencyKey); err != nil {
		return CreateCameraResult{}, fmt.Errorf("expire camera idempotency key: %w", err)
	}
	hash, err := hashCreateCamera(command)
	if err != nil {
		return CreateCameraResult{}, err
	}
	var storedHash string
	var storedResponse []byte
	err = tx.QueryRow(ctx, `SELECT request_hash, response_body FROM platform.idempotency_keys WHERE tenant_id = $1::uuid AND idempotency_key = $2`, command.TenantID, command.IdempotencyKey).Scan(&storedHash, &storedResponse)
	if err == nil {
		if storedHash != hash {
			return CreateCameraResult{}, ErrIdempotencyConflict
		}
		var camera Camera
		if err := json.Unmarshal(storedResponse, &camera); err != nil {
			return CreateCameraResult{}, fmt.Errorf("decode idempotent camera response: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return CreateCameraResult{}, fmt.Errorf("commit camera replay: %w", err)
		}
		return CreateCameraResult{Camera: camera, Replayed: true}, nil
	}
	if err != pgx.ErrNoRows {
		return CreateCameraResult{}, fmt.Errorf("read camera idempotency key: %w", err)
	}
	var siteExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM config.sites WHERE id = $1::uuid AND tenant_id = $2::uuid AND status <> 'retired')`, command.SiteID, command.TenantID).Scan(&siteExists); err != nil {
		return CreateCameraResult{}, fmt.Errorf("validate camera site: %w", err)
	}
	if !siteExists {
		return CreateCameraResult{}, ErrSiteNotFound
	}

	camera := Camera{
		ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID,
		SerialNumber: normalizeSerial(command.SerialNumber), Name: command.Name,
		GroupName: command.GroupName, Tags: normalizeTags(command.Tags), LifecycleStatus: "pending", ConfigVersion: 1,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO config.cameras (id, tenant_id, site_id, serial_number, name, group_name, tags, created_by, updated_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, NULLIF($6, ''), $7, $8, $8)
		RETURNING created_at, updated_at`,
		camera.ID, camera.TenantID, camera.SiteID, camera.SerialNumber, camera.Name, camera.GroupName, camera.Tags, command.ActorID,
	).Scan(&camera.CreatedAt, &camera.UpdatedAt); err != nil {
		return CreateCameraResult{}, classifyCameraWrite(err)
	}
	response, err := json.Marshal(camera)
	if err != nil {
		return CreateCameraResult{}, fmt.Errorf("encode camera response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.idempotency_keys (tenant_id, idempotency_key, request_hash, response_status, resource_type, resource_id, response_body)
		VALUES ($1::uuid, $2, $3, 201, 'camera', $4::uuid, $5::jsonb)`, command.TenantID, command.IdempotencyKey, hash, camera.ID, response); err != nil {
		return CreateCameraResult{}, fmt.Errorf("store idempotent camera response: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID, Action: "camera.created", ResourceType: "camera",
		ResourceID: camera.ID, RequestID: command.RequestID, AfterState: camera, OccurredAt: camera.CreatedAt,
	}); err != nil {
		return CreateCameraResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateCameraResult{}, fmt.Errorf("commit camera creation: %w", err)
	}
	return CreateCameraResult{Camera: camera}, nil
}

func (r *PostgresRepository) Update(ctx context.Context, command UpdateCameraCommand) (Camera, error) {
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return Camera{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := selectCameraForUpdate(ctx, tx, command.CameraID)
	if errors.Is(err, pgx.ErrNoRows) || current.LifecycleStatus == "retired" {
		return Camera{}, ErrCameraNotFound
	}
	if err != nil {
		return Camera{}, err
	}
	if current.ConfigVersion != command.ExpectedVersion {
		return Camera{}, ErrVersionConflict
	}
	next, changed, err := applyUpdate(current, command)
	if err != nil {
		return Camera{}, err
	}
	if !changed {
		if err := tx.Commit(ctx); err != nil {
			return Camera{}, fmt.Errorf("commit unchanged camera: %w", err)
		}
		return current, nil
	}
	if err := tx.QueryRow(ctx, `
		UPDATE config.cameras SET name = $2, group_name = NULLIF($3, ''), tags = $4,
			lifecycle_status = $5, config_version = config_version + 1, updated_by = $6, updated_at = clock_timestamp()
		WHERE id = $1::uuid
		RETURNING config_version, updated_at`, next.ID, next.Name, next.GroupName, next.Tags, next.LifecycleStatus, command.ActorID,
	).Scan(&next.ConfigVersion, &next.UpdatedAt); err != nil {
		return Camera{}, classifyCameraWrite(err)
	}
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID, Action: "camera.updated", ResourceType: "camera",
		ResourceID: next.ID, RequestID: command.RequestID, BeforeState: current, AfterState: next, OccurredAt: next.UpdatedAt,
	}); err != nil {
		return Camera{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Camera{}, fmt.Errorf("commit camera update: %w", err)
	}
	return next, nil
}

func (r *PostgresRepository) Retire(ctx context.Context, command RetireCameraCommand) (Camera, error) {
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite)
	if err != nil {
		return Camera{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := selectCameraForUpdate(ctx, tx, command.CameraID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Camera{}, ErrCameraNotFound
	}
	if err != nil {
		return Camera{}, err
	}
	if current.LifecycleStatus == "retired" {
		if err := tx.Commit(ctx); err != nil {
			return Camera{}, fmt.Errorf("commit retired camera replay: %w", err)
		}
		return current, nil
	}
	next := cloneCamera(current)
	if err := tx.QueryRow(ctx, `
		UPDATE config.cameras SET lifecycle_status = 'retired', config_version = config_version + 1,
			updated_by = $2, updated_at = clock_timestamp() WHERE id = $1::uuid
		RETURNING config_version, updated_at`, next.ID, command.ActorID).Scan(&next.ConfigVersion, &next.UpdatedAt); err != nil {
		return Camera{}, classifyCameraWrite(err)
	}
	next.LifecycleStatus = "retired"
	if _, err := audit.Append(ctx, tx, audit.Event{
		TenantID: command.TenantID, ActorID: command.ActorID, Action: "camera.retired", ResourceType: "camera",
		ResourceID: next.ID, RequestID: command.RequestID, BeforeState: current, AfterState: next, OccurredAt: next.UpdatedAt,
	}); err != nil {
		return Camera{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Camera{}, fmt.Errorf("commit camera retirement: %w", err)
	}
	return next, nil
}

const cameraSelect = `
	SELECT id::text, tenant_id::text, site_id::text, serial_number, name, COALESCE(group_name, ''),
		tags, lifecycle_status, config_version, created_at, updated_at
	FROM config.cameras`

type rowScanner interface{ Scan(...any) error }

func scanCamera(row rowScanner) (Camera, error) {
	var camera Camera
	if err := row.Scan(&camera.ID, &camera.TenantID, &camera.SiteID, &camera.SerialNumber, &camera.Name, &camera.GroupName, &camera.Tags, &camera.LifecycleStatus, &camera.ConfigVersion, &camera.CreatedAt, &camera.UpdatedAt); err != nil {
		return Camera{}, err
	}
	return camera, nil
}

func selectCameraForUpdate(ctx context.Context, tx pgx.Tx, cameraID string) (Camera, error) {
	return scanCamera(tx.QueryRow(ctx, cameraSelect+" WHERE id = $1::uuid FOR UPDATE", cameraID))
}

func (r *PostgresRepository) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("postgres camera repository is unavailable")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid verified tenant identifier: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("begin camera transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set camera tenant context: %w", err)
	}
	return tx, nil
}

func classifyCameraWrite(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return ErrSiteNotFound
		case "23505":
			return ErrSerialConflict
		}
	}
	return fmt.Errorf("write camera: %w", err)
}
