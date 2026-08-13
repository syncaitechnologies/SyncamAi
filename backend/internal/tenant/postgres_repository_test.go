package tenant

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/database"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresRepositoryEnforcesRLSIdempotencyAndAudit(t *testing.T) {
	if os.Getenv("SYNCAM_RUN_INTEGRATION") != "1" {
		t.Skip("set SYNCAM_RUN_INTEGRATION=1 to run Testcontainers integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := postgrescontainer.Run(ctx, "postgres:16-alpine",
		postgrescontainer.WithDatabase("syncam"),
		postgrescontainer.WithUsername("syncam_admin"),
		postgrescontainer.WithPassword(uuid.NewString()),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start Postgres testcontainer: %v", err)
	}
	t.Cleanup(func() {
		terminateContext, terminateCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer terminateCancel()
		if err := container.Terminate(terminateContext); err != nil {
			t.Errorf("terminate Postgres testcontainer: %v", err)
		}
	})

	adminURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()

	appPassword := uuid.NewString()
	var createRoleSQL string
	if err := adminPool.QueryRow(ctx, `
		SELECT format(
			'CREATE ROLE syncam_app LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS',
			$1
		)`, appPassword).Scan(&createRoleSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, createRoleSQL); err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyMigrations(ctx, adminPool); err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyMigrations(ctx, adminPool); err != nil {
		t.Fatalf("migrations must be idempotent: %v", err)
	}

	tenantA := "11111111-1111-4111-8111-111111111111"
	tenantB := "22222222-2222-4222-8222-222222222222"
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO identity.tenants (id, name, slug) VALUES
			($1::uuid, 'Tenant A', 'tenant-a'),
			($2::uuid, 'Tenant B', 'tenant-b')`, tenantA, tenantB); err != nil {
		t.Fatal(err)
	}

	appURL, err := withDatabaseUser(adminURL, "syncam_app", appPassword)
	if err != nil {
		t.Fatal(err)
	}
	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	defer appPool.Close()
	repository := NewPostgresRepository(appPool)

	requestID := uuid.NewString()
	commandA := CreateSiteCommand{
		TenantID: tenantA, ActorID: "user-a", RequestID: requestID,
		IdempotencyKey: "create-pilot", Name: "Pilot", Address: "Pune", Timezone: "Asia/Kolkata",
	}
	created, err := repository.CreateSite(ctx, commandA)
	if err != nil {
		t.Fatal(err)
	}
	if created.Replayed || created.Site.TenantID != tenantA || created.Site.Status != "provisioning" {
		t.Fatalf("unexpected create result: %+v", created)
	}
	replayed, err := repository.CreateSite(ctx, commandA)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Site != created.Site {
		t.Fatalf("exact replay changed response: created=%+v replayed=%+v", created, replayed)
	}
	different := commandA
	different.Name = "Different"
	if _, err := repository.CreateSite(ctx, different); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}

	commandB := commandA
	commandB.TenantID = tenantB
	commandB.RequestID = uuid.NewString()
	commandB.IdempotencyKey = "create-tenant-b"
	commandB.Name = "Tenant B site"
	if _, err := repository.CreateSite(ctx, commandB); err != nil {
		t.Fatal(err)
	}
	sitesA, err := repository.ListSites(ctx, tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if len(sitesA) != 1 || sitesA[0].TenantID != tenantA || sitesA[0].ID != created.Site.ID {
		t.Fatalf("cross-tenant rows leaked: %+v", sitesA)
	}

	var rowsWithoutTenant int
	if err := appPool.QueryRow(ctx, "SELECT count(*) FROM config.sites").Scan(&rowsWithoutTenant); err != nil {
		t.Fatal(err)
	}
	if rowsWithoutTenant != 0 {
		t.Fatalf("RLS must fail closed without transaction tenant context, got %d rows", rowsWithoutTenant)
	}

	auditTx, err := appPool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = auditTx.Rollback(ctx) }()
	if _, err := auditTx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantA); err != nil {
		t.Fatal(err)
	}
	var auditCount int
	if err := auditTx.QueryRow(ctx, `
		SELECT count(*) FROM audit.events
		WHERE tenant_id = $1::uuid AND action = 'site.created'`, tenantA).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected one audit row after exact replay, got %d", auditCount)
	}
	if _, err := auditTx.Exec(ctx, "UPDATE audit.events SET action = 'tampered' WHERE tenant_id = $1::uuid", tenantA); err == nil {
		t.Fatal("append-only audit trigger allowed an update")
	}
}

func withDatabaseUser(connectionString, username, password string) (string, error) {
	parsed, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("parse database URL: %w", err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String(), nil
}
