// Command control-plane is the future composite control-plane deployable.
// Product behavior is intentionally deferred until Phase 1 contracts are approved.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/httpapi"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
)

func main() {
	issuer := strings.TrimSpace(os.Getenv("SYNCAM_OIDC_ISSUER"))
	audience := strings.TrimSpace(os.Getenv("SYNCAM_OIDC_AUDIENCE"))
	if issuer == "" || audience == "" {
		log.Fatal("SYNCAM_OIDC_ISSUER and SYNCAM_OIDC_AUDIENCE are required")
	}
	databaseURL := strings.TrimSpace(os.Getenv("SYNCAM_DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("SYNCAM_DATABASE_URL is required")
	}
	claimTokens, err := device.NewClaimTokenManagerFromBase64(os.Getenv("SYNCAM_DEVICE_CLAIM_KEY"))
	if err != nil {
		log.Fatal("SYNCAM_DEVICE_CLAIM_KEY must be a base64url-encoded key of at least 32 bytes")
	}

	discoveryContext, cancelDiscovery := context.WithTimeout(context.Background(), 10*time.Second)
	verifier, err := identity.NewOIDCVerifier(discoveryContext, issuer, audience)
	cancelDiscovery()
	if err != nil {
		log.Fatalf("configure OIDC verifier: %v", err)
	}

	databaseContext, cancelDatabase := context.WithTimeout(context.Background(), 10*time.Second)
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		cancelDatabase()
		log.Fatalf("configure Postgres pool: %v", err)
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "syncam-control-plane"
	pool, err := pgxpool.NewWithConfig(databaseContext, poolConfig)
	if err != nil {
		cancelDatabase()
		log.Fatalf("open Postgres pool: %v", err)
	}
	if err := pool.Ping(databaseContext); err != nil {
		pool.Close()
		cancelDatabase()
		log.Fatalf("connect Postgres: %v", err)
	}
	cancelDatabase()
	defer pool.Close()

	repository := tenant.NewPostgresRepository(pool)
	eventRepository := eventing.NewPostgresRepository(pool)
	alertRepository := alerting.NewPostgresRepository(pool)
	realtimeRepository := realtime.NewPostgresRepository(pool)
	cameraRepository := device.NewPostgresRepository(pool)
	enrollmentRepository := device.NewPostgresEnrollmentRepository(pool, claimTokens)
	tickets := realtime.NewMemoryTicketStore()
	server := &http.Server{
		Addr:              envOrDefault("SYNCAM_HTTP_ADDR", ":8080"),
		Handler:           httpapi.NewWithDeviceEnrollment(verifier, repository, eventRepository, alertRepository, realtimeRepository, tickets, cameraRepository, enrollmentRepository),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("control-plane shutdown: %v", err)
		}
	}()

	log.Printf("control-plane listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("control-plane server: %v", err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
