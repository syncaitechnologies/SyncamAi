// Command lifecycle-delivery-worker delivers durable user-lifecycle intents.
// It is intentionally separate from the HTTP control plane because it holds a
// Supabase secret at runtime.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/usermanagement"
)

func main() {
	databaseURL := strings.TrimSpace(os.Getenv("SYNCAM_DATABASE_URL"))
	tenantID := strings.TrimSpace(os.Getenv("SYNCAM_WORKER_TENANT_ID"))
	projectURL := strings.TrimSpace(os.Getenv("SYNCAM_SUPABASE_URL"))
	secretKey := strings.TrimSpace(os.Getenv("SYNCAM_SUPABASE_SECRET_KEY"))
	if databaseURL == "" || tenantID == "" || projectURL == "" || secretKey == "" {
		log.Fatal("SYNCAM_DATABASE_URL, SYNCAM_WORKER_TENANT_ID, SYNCAM_SUPABASE_URL, and SYNCAM_SUPABASE_SECRET_KEY are required")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		log.Fatalf("SYNCAM_WORKER_TENANT_ID must be a UUID: %v", err)
	}
	provider, err := usermanagement.NewSupabaseInvitationProvider(projectURL, secretKey, nil)
	if err != nil {
		log.Fatal("SYNCAM_SUPABASE_URL or SYNCAM_SUPABASE_SECRET_KEY is invalid")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("open Postgres pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("connect Postgres: %v", err)
	}

	worker := usermanagement.DeliveryWorker{
		Store: usermanagement.NewPostgresDeliveryStore(pool), Provider: provider,
		WorkerID: uuid.NewString(), BatchSize: 25,
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		result, err := worker.DispatchTenant(ctx, tenantID)
		if result.Claimed > 0 {
			log.Printf("lifecycle delivery claimed=%d delivered=%d failed=%d reconciliation_required=%d", result.Claimed, result.Delivered, result.Failed, result.ReconciliationRequired)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("dispatch lifecycle delivery: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
