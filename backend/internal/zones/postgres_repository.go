package zones

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/audit"
)

type transactionPool interface { BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) }
type PostgresRepository struct{ pool transactionPool }
func NewPostgresRepository(pool transactionPool) *PostgresRepository { return &PostgresRepository{pool: pool} }

func (r *PostgresRepository) List(ctx context.Context, tenantID, siteID string) ([]Zone, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly); if err != nil { return nil, err }; defer func(){ _ = tx.Rollback(ctx) }()
	query, args := zoneSelect+" ORDER BY id LIMIT 500", []any{}
	if siteID != "" { if _, err := uuid.Parse(siteID); err != nil { return nil, fmt.Errorf("invalid verified site identifier: %w", err) }; query, args = zoneSelect+" WHERE site_id = $1::uuid ORDER BY id LIMIT 500", []any{siteID} }
	rows, err := tx.Query(ctx, query, args...); if err != nil { return nil, fmt.Errorf("list zones: %w", err) }; defer rows.Close()
	result := make([]Zone, 0); for rows.Next() { zone, err := scanZone(rows); if err != nil { return nil, err }; result = append(result, zone) }; if err := rows.Err(); err != nil { return nil, fmt.Errorf("iterate zones: %w", err) }; if err := tx.Commit(ctx); err != nil { return nil, fmt.Errorf("commit zone list: %w", err) }; return result, nil
}

func (r *PostgresRepository) Get(ctx context.Context, tenantID, zoneID string) (Zone, error) {
	tx, err := r.begin(ctx, tenantID, pgx.ReadOnly); if err != nil { return Zone{}, err }; defer func(){ _ = tx.Rollback(ctx) }()
	zone, err := scanZone(tx.QueryRow(ctx, zoneSelect+" WHERE id = $1::uuid", zoneID)); if errors.Is(err, pgx.ErrNoRows) { return Zone{}, ErrNotFound }; if err != nil { return Zone{}, err }; if err := tx.Commit(ctx); err != nil { return Zone{}, fmt.Errorf("commit zone read: %w", err) }; return zone, nil
}

func (r *PostgresRepository) Create(ctx context.Context, command CreateCommand) (CreateResult, error) {
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite); if err != nil { return CreateResult{}, err }; defer func(){ _ = tx.Rollback(ctx) }()
	lockKey := command.TenantID+":"+command.IdempotencyKey; if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil { return CreateResult{}, fmt.Errorf("lock zone idempotency key: %w", err) }
	if _, err := tx.Exec(ctx, `DELETE FROM platform.idempotency_keys WHERE tenant_id = $1::uuid AND idempotency_key = $2 AND expires_at <= clock_timestamp()`, command.TenantID, command.IdempotencyKey); err != nil { return CreateResult{}, fmt.Errorf("expire zone idempotency key: %w", err) }
	hash, err := hashCreate(command); if err != nil { return CreateResult{}, err }; var storedHash string; var storedResponse []byte
	err = tx.QueryRow(ctx, `SELECT request_hash, response_body FROM platform.idempotency_keys WHERE tenant_id = $1::uuid AND idempotency_key = $2`, command.TenantID, command.IdempotencyKey).Scan(&storedHash, &storedResponse)
	if err == nil { if storedHash != hash { return CreateResult{}, ErrIdempotencyConflict }; var zone Zone; if err := json.Unmarshal(storedResponse, &zone); err != nil { return CreateResult{}, fmt.Errorf("decode idempotent zone response: %w", err) }; if err := tx.Commit(ctx); err != nil { return CreateResult{}, fmt.Errorf("commit zone replay: %w", err) }; return CreateResult{Zone: zone, Replayed: true}, nil }
	if err != pgx.ErrNoRows { return CreateResult{}, fmt.Errorf("read zone idempotency key: %w", err) }
	var siteExists bool; if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM config.sites WHERE id = $1::uuid AND tenant_id = $2::uuid AND status <> 'retired')`, command.SiteID, command.TenantID).Scan(&siteExists); err != nil { return CreateResult{}, fmt.Errorf("validate zone site: %w", err) }; if !siteExists { return CreateResult{}, ErrSiteNotFound }
	zone := Zone{ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID, CameraID: command.CameraID, Floor: command.Floor, Name: command.Name, Kind: command.Kind, Geometry: command.Geometry, Enabled: command.Enabled, ConfigVersion: 1}
	if err := tx.QueryRow(ctx, `INSERT INTO config.zones (id, tenant_id, site_id, camera_id, floor, name, kind, geometry, enabled, created_by, updated_by) VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4, '')::uuid, NULLIF($5, ''), $6, $7, $8::jsonb, $9, $10, $10) RETURNING created_at, updated_at`, zone.ID, zone.TenantID, zone.SiteID, zone.CameraID, zone.Floor, zone.Name, zone.Kind, zone.Geometry, zone.Enabled, command.ActorID).Scan(&zone.CreatedAt, &zone.UpdatedAt); err != nil { return CreateResult{}, fmt.Errorf("write zone: %w", err) }
	response, err := json.Marshal(zone); if err != nil { return CreateResult{}, fmt.Errorf("encode zone response: %w", err) }; if _, err := tx.Exec(ctx, `INSERT INTO platform.idempotency_keys (tenant_id, idempotency_key, request_hash, response_status, resource_type, resource_id, response_body) VALUES ($1::uuid, $2, $3, 201, 'zone', $4::uuid, $5::jsonb)`, command.TenantID, command.IdempotencyKey, hash, zone.ID, response); err != nil { return CreateResult{}, fmt.Errorf("store zone idempotency key: %w", err) }
	if err := audit.Append(ctx, tx, audit.Event{TenantID: command.TenantID, ActorID: command.ActorID, Action: "zone.created", ResourceType: "zone", ResourceID: zone.ID, RequestID: command.RequestID, AfterState: zone, OccurredAt: zone.CreatedAt}); err != nil { return CreateResult{}, err }; if err := tx.Commit(ctx); err != nil { return CreateResult{}, fmt.Errorf("commit zone creation: %w", err) }; return CreateResult{Zone: zone}, nil
}

func (r *PostgresRepository) Update(ctx context.Context, command UpdateCommand) (Zone, error) {
	tx, err := r.begin(ctx, command.TenantID, pgx.ReadWrite); if err != nil { return Zone{}, err }; defer func(){ _ = tx.Rollback(ctx) }()
	current, err := scanZone(tx.QueryRow(ctx, zoneSelect+" WHERE id = $1::uuid FOR UPDATE", command.ZoneID)); if errors.Is(err, pgx.ErrNoRows) { return Zone{}, ErrNotFound }; if err != nil { return Zone{}, err }; if current.ConfigVersion != command.ExpectedVersion { return Zone{}, ErrVersionConflict }
	next := clone(current); if command.Name != nil { next.Name = *command.Name }; if command.Floor != nil { next.Floor = *command.Floor }; if command.Geometry != nil { next.Geometry = append(json.RawMessage(nil), (*command.Geometry)...)}; if command.Enabled != nil { next.Enabled = *command.Enabled }; if same(current, next) { if err := tx.Commit(ctx); err != nil { return Zone{}, fmt.Errorf("commit unchanged zone: %w", err) }; return current, nil }
	if err := tx.QueryRow(ctx, `UPDATE config.zones SET floor = NULLIF($2, ''), name = $3, geometry = $4::jsonb, enabled = $5, config_version = config_version + 1, updated_by = $6, updated_at = clock_timestamp() WHERE id = $1::uuid RETURNING config_version, updated_at`, next.ID, next.Floor, next.Name, next.Geometry, next.Enabled, command.ActorID).Scan(&next.ConfigVersion, &next.UpdatedAt); err != nil { return Zone{}, fmt.Errorf("write zone: %w", err) }
	if err := audit.Append(ctx, tx, audit.Event{TenantID: command.TenantID, ActorID: command.ActorID, Action: "zone.updated", ResourceType: "zone", ResourceID: next.ID, RequestID: command.RequestID, BeforeState: current, AfterState: next, OccurredAt: next.UpdatedAt}); err != nil { return Zone{}, err }; if err := tx.Commit(ctx); err != nil { return Zone{}, fmt.Errorf("commit zone update: %w", err) }; return next, nil
}

const zoneSelect = `SELECT id::text, tenant_id::text, site_id::text, COALESCE(camera_id::text, ''), COALESCE(floor, ''), name, kind, geometry, enabled, config_version, created_at, updated_at FROM config.zones`
type rowScanner interface { Scan(...any) error }
func scanZone(row rowScanner) (Zone, error) { var zone Zone; if err := row.Scan(&zone.ID, &zone.TenantID, &zone.SiteID, &zone.CameraID, &zone.Floor, &zone.Name, &zone.Kind, &zone.Geometry, &zone.Enabled, &zone.ConfigVersion, &zone.CreatedAt, &zone.UpdatedAt); err != nil { return Zone{}, err }; return zone, nil }
func (r *PostgresRepository) begin(ctx context.Context, tenantID string, mode pgx.TxAccessMode) (pgx.Tx, error) { if r == nil || r.pool == nil { return nil, errors.New("postgres zone repository is unavailable") }; if _, err := uuid.Parse(tenantID); err != nil { return nil, fmt.Errorf("invalid verified tenant identifier: %w", err) }; tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode}); if err != nil { return nil, fmt.Errorf("begin zone transaction: %w", err) }; if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil { _ = tx.Rollback(ctx); return nil, fmt.Errorf("set zone tenant context: %w", err) }; return tx, nil }
