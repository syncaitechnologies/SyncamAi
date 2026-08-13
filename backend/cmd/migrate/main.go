// Command migrate applies the embedded Postgres schema using an administrative connection.
package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/database"
)

func main() {
	databaseURL := strings.TrimSpace(os.Getenv("SYNCAM_MIGRATION_DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("SYNCAM_MIGRATION_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("configure migration database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("connect migration database: %v", err)
	}
	if err := database.ApplyMigrations(ctx, pool); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}
	log.Print("database migrations applied")
}
