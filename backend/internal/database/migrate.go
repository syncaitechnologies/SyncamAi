// Package database retains the historical embedded migration helper for old
// deterministic repository tests. Supabase CLI migrations are authoritative.
package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migrationPool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Begin(context.Context) (pgx.Tx, error)
}

// ApplyMigrations applies the pre-Supabase historical schema only for legacy
// test compatibility. Production and development schema operations use the
// Supabase CLI migration history in backend/supabase/migrations.
func ApplyMigrations(ctx context.Context, pool migrationPool) error {
	if pool == nil {
		return fmt.Errorf("database pool is required")
	}
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS platform"); err != nil {
		return fmt.Errorf("initialize migration schema: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS platform.schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
		)`); err != nil {
		return fmt.Errorf("initialize schema migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := applyMigration(ctx, pool, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, pool migrationPool, version string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var applied bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM platform.schema_migrations WHERE version = $1)",
		version,
	).Scan(&applied); err != nil {
		return fmt.Errorf("check migration %s: %w", version, err)
	}
	if applied {
		return tx.Commit(ctx)
	}

	sql, err := migrationFiles.ReadFile("migrations/" + version)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, string(sql), pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO platform.schema_migrations (version) VALUES ($1)", version,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}
