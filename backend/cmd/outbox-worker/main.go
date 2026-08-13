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
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/outbox"
)

func main() {
	databaseURL := strings.TrimSpace(os.Getenv("SYNCAM_DATABASE_URL"))
	tenantID := strings.TrimSpace(os.Getenv("SYNCAM_WORKER_TENANT_ID"))
	if databaseURL == "" || tenantID == "" {
		log.Fatal("SYNCAM_DATABASE_URL and SYNCAM_WORKER_TENANT_ID are required")
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		log.Fatalf("SYNCAM_WORKER_TENANT_ID must be a UUID: %v", err)
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
	dispatcher := outbox.Dispatcher{
		Store: outbox.NewPostgresStore(pool), Publisher: alerting.NewProjector(pool),
		WorkerID: uuid.NewString(), BatchSize: 25,
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		result, err := dispatcher.DispatchTenant(ctx, tenantID)
		if err != nil && ctx.Err() == nil {
			log.Printf("dispatch outbox: %v", err)
		}
		if result.Claimed > 0 {
			log.Printf("outbox claimed=%d published=%d failed=%d", result.Claimed, result.Published, result.Failed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
