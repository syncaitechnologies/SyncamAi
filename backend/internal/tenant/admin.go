package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Owner admin screen (ADR-018): platform operators list every workspace and
// change its plan, trial end, limits and live-view minutes. The database
// routines re-check that the actor is an operator and audit every change.

var (
	// ErrNotOperator means the actor is not in platform.operators.
	ErrNotOperator = errors.New("not a platform operator")
	// ErrAdminInvalid means a change was refused as invalid.
	ErrAdminInvalid = errors.New("invalid workspace change")
	// ErrWorkspaceNotFound means the workspace does not exist.
	ErrWorkspaceNotFound = errors.New("workspace not found")
)

// AdminWorkspace is one workspace as the owner admin screen shows it.
type AdminWorkspace struct {
	TenantID        string     `json:"tenant_id"`
	Name            string     `json:"name"`
	Slug            string     `json:"slug"`
	Plan            string     `json:"plan"`
	TrialEndsAt     *time.Time `json:"trial_ends_at"`
	SiteLimit       *int       `json:"site_limit"`
	CameraLimit     *int       `json:"camera_limit"`
	LiveViewMinutes *int       `json:"live_view_minutes"`
	LiveViewSeconds int64      `json:"live_view_seconds"`
	Sites           int64      `json:"sites"`
	Cameras         int64      `json:"cameras"`
	CreatedAt       time.Time  `json:"created_at"`
	// Features says which plan-controlled features the workspace's plan
	// includes (ADR-019), filled in by the API from the feature catalog.
	Features map[string]bool `json:"features,omitempty"`
}

// AdminRepository lists and changes workspaces for platform operators.
type AdminRepository interface {
	ListWorkspaces(ctx context.Context, actorID string) ([]AdminWorkspace, error)
	UpdateWorkspace(ctx context.Context, actorID, requestID, tenantID string, changes map[string]any) error
}

// AdminFeatureRepository reads and sets which features each workspace's plan
// includes (ADR-019). Only the owner's choices that differ from the catalog
// default are stored.
type AdminFeatureRepository interface {
	ListPlanFeatures(ctx context.Context, actorID string) (map[string]map[string]bool, error)
	SetPlanFeatures(ctx context.Context, actorID, requestID, tenantID string, changes map[string]bool) error
}

// ListPlanFeatures returns the owner's per-workspace choices, keyed by
// workspace then feature.
func (r *PostgresRepository) ListPlanFeatures(ctx context.Context, actorID string) (map[string]map[string]bool, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("postgres tenant repository is unavailable")
	}
	if _, err := uuid.Parse(actorID); err != nil {
		return nil, ErrNotOperator
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin plan feature list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT tenant_id::text, feature, included FROM platform.admin_list_features($1::uuid)`, actorID)
	if err != nil {
		return nil, classifyAdminError(err)
	}
	result := map[string]map[string]bool{}
	for rows.Next() {
		var tenantID, feature string
		var included bool
		if err := rows.Scan(&tenantID, &feature, &included); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read plan features: %w", err)
		}
		if result[tenantID] == nil {
			result[tenantID] = map[string]bool{}
		}
		result[tenantID][feature] = included
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, classifyAdminError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit plan feature list: %w", err)
	}
	return result, nil
}

// SetPlanFeatures changes which features a workspace's plan includes and
// audits it in the database.
func (r *PostgresRepository) SetPlanFeatures(ctx context.Context, actorID, requestID, tenantID string, changes map[string]bool) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("postgres tenant repository is unavailable")
	}
	for _, value := range []string{actorID, requestID, tenantID} {
		if _, err := uuid.Parse(value); err != nil {
			return ErrAdminInvalid
		}
	}
	payload, err := json.Marshal(changes)
	if err != nil || len(changes) == 0 {
		return ErrAdminInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return fmt.Errorf("begin plan feature change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT platform.admin_set_features($1::uuid, $2::uuid, $3::uuid, $4::jsonb)`,
		actorID, requestID, tenantID, string(payload)); err != nil {
		return classifyAdminError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit plan feature change: %w", err)
	}
	return nil
}

func optionalInt(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func classifyAdminError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "42501":
			return ErrNotOperator
		case "22023":
			return ErrAdminInvalid
		case "P0002":
			return ErrWorkspaceNotFound
		}
	}
	return err
}

// ListWorkspaces returns every workspace, newest first.
func (r *PostgresRepository) ListWorkspaces(ctx context.Context, actorID string) ([]AdminWorkspace, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("postgres tenant repository is unavailable")
	}
	if _, err := uuid.Parse(actorID); err != nil {
		return nil, ErrNotOperator
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin workspace list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT tenant_id::text, name, slug, plan, trial_ends_at, site_limit, camera_limit, live_view_minutes,
		       live_view_seconds, sites, cameras, created_at
		FROM platform.admin_list_workspaces($1::uuid)`, actorID)
	if err != nil {
		return nil, classifyAdminError(err)
	}
	result := []AdminWorkspace{}
	for rows.Next() {
		var item AdminWorkspace
		var siteLimit, cameraLimit, liveMinutes *int32
		if err := rows.Scan(&item.TenantID, &item.Name, &item.Slug, &item.Plan, &item.TrialEndsAt, &siteLimit, &cameraLimit,
			&liveMinutes, &item.LiveViewSeconds, &item.Sites, &item.Cameras, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read workspace list: %w", err)
		}
		item.SiteLimit, item.CameraLimit, item.LiveViewMinutes = optionalInt(siteLimit), optionalInt(cameraLimit), optionalInt(liveMinutes)
		result = append(result, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, classifyAdminError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit workspace list: %w", err)
	}
	return result, nil
}

// UpdateWorkspace applies validated changes and audits them in the database.
func (r *PostgresRepository) UpdateWorkspace(ctx context.Context, actorID, requestID, tenantID string, changes map[string]any) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("postgres tenant repository is unavailable")
	}
	for _, value := range []string{actorID, requestID, tenantID} {
		if _, err := uuid.Parse(value); err != nil {
			return ErrAdminInvalid
		}
	}
	payload, err := json.Marshal(changes)
	if err != nil || len(changes) == 0 {
		return ErrAdminInvalid
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return fmt.Errorf("begin workspace change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT platform.admin_update_workspace($1::uuid, $2::uuid, $3::uuid, $4::jsonb)`,
		actorID, requestID, tenantID, string(payload)); err != nil {
		return classifyAdminError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit workspace change: %w", err)
	}
	return nil
}
