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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/configdelivery"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/httpapi"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/identity"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/outbox"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/privacymasks"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/realtime"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

func main() {
	issuer := strings.TrimSpace(os.Getenv("SYNCAM_OIDC_ISSUER"))
	audience := strings.TrimSpace(os.Getenv("SYNCAM_OIDC_AUDIENCE"))
	profile := identity.OIDCProfile(strings.TrimSpace(os.Getenv("SYNCAM_OIDC_PROFILE")))
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
	verifier, err := identity.NewOIDCVerifierForProfile(discoveryContext, profile, issuer, audience)
	cancelDiscovery()
	if err != nil {
		log.Fatal("configure OIDC verifier failed")
	}

	databaseContext, cancelDatabase := context.WithTimeout(context.Background(), 10*time.Second)
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		cancelDatabase()
		log.Fatal("configure Postgres pool failed")
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "syncam-control-plane"
	pool, err := pgxpool.NewWithConfig(databaseContext, poolConfig)
	if err != nil {
		cancelDatabase()
		log.Fatal("open Postgres pool failed")
	}
	if err := pool.Ping(databaseContext); err != nil {
		pool.Close()
		cancelDatabase()
		log.Fatal("connect Postgres failed")
	}
	cancelDatabase()
	defer pool.Close()

	repository := tenant.NewPostgresRepository(pool)
	eventRepository := eventing.NewPostgresRepository(pool)
	alertRepository := alerting.NewPostgresRepository(pool)
	realtimeRepository := realtime.NewPostgresRepository(pool)
	cameraRepository := device.NewPostgresRepository(pool)
	enrollmentRepository := device.NewPostgresEnrollmentRepository(pool, claimTokens)
	deviceStatusRepository := device.NewPostgresStatusRepository(pool)
	zoneRepository := zones.NewPostgresRepository(pool)
	configurationRepository := configdelivery.NewPostgresRepository(pool)
	privacyMaskRepository := privacymasks.NewPostgresRepository(pool)
	tickets := realtime.NewMemoryTicketStore()

	runContext, stopRunning := context.WithCancel(context.Background())
	defer stopRunning()
	siteIngest := httpapi.SiteIngestOptions{
		Keys:          siteingest.NewPostgresKeyStore(pool),
		AlertEvidence: alertRepository,
	}
	if supabaseURL, secretKey := strings.TrimSpace(os.Getenv("SYNCAM_SUPABASE_URL")), strings.TrimSpace(os.Getenv("SYNCAM_SUPABASE_SECRET_KEY")); supabaseURL != "" && secretKey != "" {
		siteIngest.Evidence = &siteingest.SupabaseEvidenceStore{BaseURL: supabaseURL, SecretKey: secretKey, Bucket: envOrDefault("SYNCAM_EVIDENCE_BUCKET", "snapshots")}
		log.Print("alert snapshot storage enabled")
	} else {
		log.Print("alert snapshot storage disabled: set SYNCAM_SUPABASE_URL and SYNCAM_SUPABASE_SECRET_KEY")
	}
	var notifier alerting.Notifier
	if botToken := strings.TrimSpace(os.Getenv("SYNCAM_TELEGRAM_BOT_TOKEN")); botToken != "" {
		telegram := &siteingest.TelegramClient{Token: botToken}
		targets := siteingest.NewPostgresTargetStore(pool)
		siteIngest.TelegramTargets = targets
		siteIngest.TelegramBotHandle = strings.TrimPrefix(strings.TrimSpace(os.Getenv("SYNCAM_TELEGRAM_BOT_HANDLE")), "@")
		notifier = &siteingest.TelegramNotifier{
			Client: telegram, Targets: targets, Evidence: siteIngest.Evidence,
			Describe: siteingest.NewRepositoryDescriber(repository, cameraRepository, zoneRepository),
			Logf:     log.Printf,
		}
		go siteingest.RunTelegramLinker(runContext, telegram, targets, siteingest.SiteNamer(repository), log.Printf)
		log.Print("telegram alerts enabled")
	} else {
		log.Print("telegram alerts disabled: set SYNCAM_TELEGRAM_BOT_TOKEN")
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SYNCAM_INPROCESS_OUTBOX")), "true") {
		store := outbox.NewPostgresStore(pool)
		dispatcher := outbox.Dispatcher{Store: store, Publisher: alerting.NewProjectorWithNotifier(pool, notifier), WorkerID: uuid.NewString(), BatchSize: 25}
		go outbox.RunAllTenants(runContext, dispatcher, store, time.Second, log.Printf)
		log.Print("in-process alert worker enabled for all tenants")
	}

	handler, err := httpapi.WithBrowserOrigins(httpapi.NewWithSiteIngest(verifier, repository, eventRepository, alertRepository, realtimeRepository, tickets, cameraRepository, enrollmentRepository, deviceStatusRepository, device.MTLSDeviceVerifier{}, zoneRepository, configurationRepository, privacyMaskRepository, privacymasks.NewPostgresReleaseTransportRepository(pool), siteIngest), os.Getenv("SYNCAM_BROWSER_ORIGINS"))
	if err != nil {
		log.Fatal("SYNCAM_BROWSER_ORIGINS is invalid; use explicit HTTPS origins (HTTP only for localhost)")
	}
	server := &http.Server{
		Addr:              envOrDefault("SYNCAM_HTTP_ADDR", ":8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stopped
		stopRunning()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Print("control-plane shutdown failed")
		}
	}()

	log.Print("control-plane listening")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("control-plane server failed")
	}
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
