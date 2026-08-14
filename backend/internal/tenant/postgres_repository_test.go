package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/database"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/eventing"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/outbox"
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
			$1::text
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
	createdPayload, err := json.Marshal(created.Site)
	if err != nil {
		t.Fatal(err)
	}
	replayedPayload, err := json.Marshal(replayed.Site)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || string(replayedPayload) != string(createdPayload) {
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
	createdB, err := repository.CreateSite(ctx, commandB)
	if err != nil {
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

	cameraRepository := device.NewPostgresRepository(appPool)
	cameraCommand := device.CreateCameraCommand{
		TenantID: tenantA, ActorID: "user-a", RequestID: uuid.NewString(), IdempotencyKey: "create-camera-a",
		SiteID: created.Site.ID, SerialNumber: "SN-A-001", Name: "Front gate", GroupName: "Perimeter", Tags: []string{"gate"},
	}
	createdCamera, err := cameraRepository.Create(ctx, cameraCommand)
	if err != nil || createdCamera.Replayed || createdCamera.Camera.LifecycleStatus != "pending" {
		t.Fatalf("camera create failed: %+v %v", createdCamera, err)
	}
	replayedCamera, err := cameraRepository.Create(ctx, cameraCommand)
	if err != nil || !replayedCamera.Replayed || replayedCamera.Camera.ID != createdCamera.Camera.ID {
		t.Fatalf("camera replay failed: %+v %v", replayedCamera, err)
	}
	crossTenantCamera := cameraCommand
	crossTenantCamera.RequestID = uuid.NewString()
	crossTenantCamera.IdempotencyKey = "cross-tenant-camera"
	crossTenantCamera.SiteID = createdB.Site.ID
	crossTenantCamera.SerialNumber = "SN-A-002"
	if _, err := cameraRepository.Create(ctx, crossTenantCamera); !errors.Is(err, device.ErrSiteNotFound) {
		t.Fatalf("cross-tenant camera site must fail, got %v", err)
	}
	active := "active"
	updatedCamera, err := cameraRepository.Update(ctx, device.UpdateCameraCommand{
		TenantID: tenantA, ActorID: "user-a", RequestID: uuid.NewString(), CameraID: createdCamera.Camera.ID,
		ExpectedVersion: createdCamera.Camera.ConfigVersion, LifecycleStatus: &active,
	})
	if err != nil || updatedCamera.LifecycleStatus != "active" || updatedCamera.ConfigVersion != 2 {
		t.Fatalf("camera activation failed: %+v %v", updatedCamera, err)
	}
	camerasA, err := cameraRepository.List(ctx, tenantA, created.Site.ID)
	if err != nil || len(camerasA) != 1 || camerasA[0].TenantID != tenantA {
		t.Fatalf("camera tenant isolation failed: %+v %v", camerasA, err)
	}
	if _, err := cameraRepository.Retire(ctx, device.RetireCameraCommand{TenantID: tenantA, ActorID: "user-a", RequestID: uuid.NewString(), CameraID: createdCamera.Camera.ID}); err != nil {
		t.Fatalf("camera retirement failed: %v", err)
	}
	var camerasWithoutTenant int
	if err := appPool.QueryRow(ctx, "SELECT count(*) FROM config.cameras").Scan(&camerasWithoutTenant); err != nil {
		t.Fatal(err)
	}
	if camerasWithoutTenant != 0 {
		t.Fatalf("camera RLS must fail closed without tenant context, got %d rows", camerasWithoutTenant)
	}

	claimTokens, err := device.NewClaimTokenManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	enrollmentRepository := device.NewPostgresEnrollmentRepository(appPool, claimTokens)
	claimCommand := device.IssueClaimCommand{
		TenantID: tenantA, ActorID: "user-a", RequestID: uuid.NewString(), IdempotencyKey: "issue-device-a",
		SiteID: created.Site.ID, SerialNumber: "EDGE-A-001", HardwareTier: "m", Model: "Jetson Orin",
	}
	issuedClaim, err := enrollmentRepository.IssueClaim(ctx, claimCommand)
	if err != nil || issuedClaim.Replayed || issuedClaim.ClaimToken == "" || issuedClaim.Claim.ExpiresAt.Sub(issuedClaim.Claim.CreatedAt) != 24*time.Hour {
		t.Fatalf("device claim issuance failed: %+v %v", issuedClaim, err)
	}
	replayedClaim, err := enrollmentRepository.IssueClaim(ctx, claimCommand)
	if err != nil || !replayedClaim.Replayed || replayedClaim.ClaimToken != issuedClaim.ClaimToken {
		t.Fatalf("device claim replay failed: %+v %v", replayedClaim, err)
	}
	crossTenantClaim := claimCommand
	crossTenantClaim.RequestID = uuid.NewString()
	crossTenantClaim.IdempotencyKey = "issue-device-cross-tenant"
	crossTenantClaim.SiteID = createdB.Site.ID
	crossTenantClaim.SerialNumber = "EDGE-A-002"
	if _, err := enrollmentRepository.IssueClaim(ctx, crossTenantClaim); !errors.Is(err, device.ErrSiteNotFound) {
		t.Fatalf("cross-tenant device site must fail, got %v", err)
	}
	activatedDevice, err := enrollmentRepository.Activate(ctx, device.ActivateDeviceCommand{
		DeviceID: issuedClaim.Claim.DeviceID, ClaimToken: issuedClaim.ClaimToken, SerialNumber: "edge-a-001", RequestID: uuid.NewString(),
	})
	if err != nil || activatedDevice.Status != "active" || activatedDevice.TenantID != tenantA || activatedDevice.SiteID != created.Site.ID {
		t.Fatalf("device activation failed: %+v %v", activatedDevice, err)
	}
	if _, err := enrollmentRepository.Activate(ctx, device.ActivateDeviceCommand{DeviceID: issuedClaim.Claim.DeviceID, ClaimToken: issuedClaim.ClaimToken, SerialNumber: "EDGE-A-001", RequestID: uuid.NewString()}); !errors.Is(err, device.ErrClaimConsumed) {
		t.Fatalf("device claim reuse must fail, got %v", err)
	}
	if _, err := adminPool.Exec(ctx, "UPDATE config.edge_devices SET cert_status = 'active' WHERE id = $1::uuid", activatedDevice.ID); err != nil {
		t.Fatal(err)
	}
	statusRepository := device.NewPostgresStatusRepository(appPool)
	heartbeatCommand := device.HeartbeatCommand{
		DeviceID: activatedDevice.ID, HeartbeatID: uuid.NewString(), ReportedAt: time.Now().UTC(),
		UptimeSeconds: 42, StoreForwardDepth: 7, FirmwareVersion: "1.2.3",
	}
	heartbeat, err := statusRepository.RecordHeartbeat(ctx, heartbeatCommand)
	if err != nil || heartbeat.Replayed || heartbeat.Device.Status != "active" || heartbeat.Device.StoreForwardDepth != 7 {
		t.Fatalf("device heartbeat failed: %+v %v", heartbeat, err)
	}
	replayedHeartbeat, err := statusRepository.RecordHeartbeat(ctx, heartbeatCommand)
	if err != nil || !replayedHeartbeat.Replayed || !replayedHeartbeat.ObservedAt.Equal(heartbeat.ObservedAt) {
		t.Fatalf("device heartbeat replay failed: %+v %v", replayedHeartbeat, err)
	}
	conflictingHeartbeat := heartbeatCommand
	conflictingHeartbeat.FirmwareVersion = "1.2.4"
	if _, err := statusRepository.RecordHeartbeat(ctx, conflictingHeartbeat); !errors.Is(err, device.ErrHeartbeatConflict) {
		t.Fatalf("device heartbeat conflict must fail, got %v", err)
	}
	listedDevices, err := statusRepository.ListDevices(ctx, tenantA, created.Site.ID, time.Now().UTC())
	if err != nil || len(listedDevices) != 1 || listedDevices[0].ID != activatedDevice.ID || listedDevices[0].Status != "active" {
		t.Fatalf("tenant-scoped device status failed: %+v %v", listedDevices, err)
	}
	for _, table := range []string{"config.edge_devices", "platform.device_claims", "edge.device_heartbeats"} {
		var count int
		if err := appPool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s RLS must fail closed without tenant context, got %d rows", table, count)
		}
	}

	eventRepository := eventing.NewPostgresRepository(appPool)
	eventCommand := eventing.IngestCommand{ActorID: "edge-a", RequestID: uuid.NewString(), Event: eventing.DetectionEvent{
		EventID: uuid.NewString(), TenantID: tenantA, DedupeKey: "camera-a:42", OccurredAt: time.Now().UTC(),
		SiteID: created.Site.ID, CameraID: uuid.NewString(), ZoneID: uuid.NewString(), EventType: "intrusion",
		ModelVersion: "detector-1", Confidence: 0.91, EvidenceRefs: []string{"evidence://clip-a"},
		RequiresHumanReview: true, ReviewState: "pending",
	}}
	accepted, err := eventRepository.Ingest(ctx, eventCommand)
	if err != nil || !accepted.Accepted || accepted.Replayed {
		t.Fatalf("event ingest failed: %+v %v", accepted, err)
	}
	replayedEvent, err := eventRepository.Ingest(ctx, eventCommand)
	if err != nil || !replayedEvent.Replayed || replayedEvent.EventID != accepted.EventID {
		t.Fatalf("event replay failed: %+v %v", replayedEvent, err)
	}
	differentEvent := eventCommand
	differentEvent.Event.Confidence = 0.5
	if _, err := eventRepository.Ingest(ctx, differentEvent); !errors.Is(err, eventing.ErrDedupeConflict) {
		t.Fatalf("expected event dedupe conflict, got %v", err)
	}
	crossTenantSite := eventCommand
	crossTenantSite.Event.EventID = uuid.NewString()
	crossTenantSite.Event.DedupeKey = "camera-a:cross-tenant-site"
	crossTenantSite.Event.SiteID = createdB.Site.ID
	if _, err := eventRepository.Ingest(ctx, crossTenantSite); !errors.Is(err, eventing.ErrSiteNotFound) {
		t.Fatalf("cross-tenant site reference must fail, got %v", err)
	}
	tenantBEvent := eventCommand
	tenantBEvent.RequestID = uuid.NewString()
	tenantBEvent.Event.EventID = uuid.NewString()
	tenantBEvent.Event.TenantID = tenantB
	tenantBEvent.Event.DedupeKey = "camera-b:42"
	tenantBEvent.Event.SiteID = createdB.Site.ID
	if _, err := eventRepository.Ingest(ctx, tenantBEvent); err != nil {
		t.Fatal(err)
	}
	dispatcher := outbox.Dispatcher{
		Store: outbox.NewPostgresStore(appPool), Publisher: alerting.NewProjector(appPool),
		WorkerID: uuid.NewString(), BatchSize: 25,
	}
	dispatched, err := dispatcher.DispatchTenant(ctx, tenantA)
	if err != nil || dispatched.Claimed != 1 || dispatched.Published != 1 || dispatched.Failed != 0 {
		t.Fatalf("alert projection dispatch failed: %+v %v", dispatched, err)
	}
	retry, err := dispatcher.DispatchTenant(ctx, tenantA)
	if err != nil || retry.Claimed != 0 {
		t.Fatalf("published message was reclaimed: %+v %v", retry, err)
	}
	alertRepository := alerting.NewPostgresRepository(appPool)
	queue, err := alertRepository.List(ctx, tenantA)
	if err != nil || len(queue) != 1 {
		t.Fatalf("projected alert unavailable: %+v %v", queue, err)
	}
	acknowledgeCommand := alerting.AcknowledgeCommand{
		TenantID: tenantA, SiteID: created.Site.ID, AlertID: queue[0].ID,
		ActorID: "operator-a", RequestID: uuid.NewString(), IdempotencyKey: "ack-projected-alert",
	}
	acknowledged, err := alertRepository.Acknowledge(ctx, acknowledgeCommand)
	if err != nil || acknowledged.Replayed || acknowledged.Alert.Status != "acknowledged" {
		t.Fatalf("alert acknowledgment failed: %+v %v", acknowledged, err)
	}
	acknowledgedReplay, err := alertRepository.Acknowledge(ctx, acknowledgeCommand)
	if err != nil || !acknowledgedReplay.Replayed || acknowledgedReplay.Alert.ID != acknowledged.Alert.ID {
		t.Fatalf("alert acknowledgment replay failed: %+v %v", acknowledgedReplay, err)
	}
	var eventsWithoutTenant int
	if err := appPool.QueryRow(ctx, "SELECT count(*) FROM events.detection_events").Scan(&eventsWithoutTenant); err != nil {
		t.Fatal(err)
	}
	if eventsWithoutTenant != 0 {
		t.Fatalf("event RLS must fail closed without tenant context, got %d rows", eventsWithoutTenant)
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
	var eventCount, outboxCount, publishedCount, alertCount, receiptCount, eventAuditCount, alertAuditCount, acknowledgmentAuditCount, cameraAuditCount, actionCount, realtimeCount int
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM events.detection_events WHERE tenant_id = $1::uuid", tenantA).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM messaging.outbox_messages WHERE tenant_id = $1::uuid", tenantA).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM messaging.outbox_messages WHERE tenant_id = $1::uuid AND published_at IS NOT NULL", tenantA).Scan(&publishedCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM alerts.alerts WHERE tenant_id = $1::uuid", tenantA).Scan(&alertCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM messaging.consumer_receipts WHERE tenant_id = $1::uuid", tenantA).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM audit.events WHERE tenant_id = $1::uuid AND action = 'event.accepted'", tenantA).Scan(&eventAuditCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM audit.events WHERE tenant_id = $1::uuid AND action = 'alert.created'", tenantA).Scan(&alertAuditCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM audit.events WHERE tenant_id = $1::uuid AND action = 'alert.acknowledged'", tenantA).Scan(&acknowledgmentAuditCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM audit.events WHERE tenant_id = $1::uuid AND action LIKE 'camera.%'", tenantA).Scan(&cameraAuditCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM alerts.alert_actions WHERE tenant_id = $1::uuid", tenantA).Scan(&actionCount); err != nil {
		t.Fatal(err)
	}
	if err := auditTx.QueryRow(ctx, "SELECT count(*) FROM realtime.messages WHERE tenant_id = $1::uuid", tenantA).Scan(&realtimeCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || outboxCount != 1 || publishedCount != 1 || alertCount != 1 || receiptCount != 1 || eventAuditCount != 1 || alertAuditCount != 1 || acknowledgmentAuditCount != 1 || cameraAuditCount != 3 || actionCount != 1 || realtimeCount != 2 {
		t.Fatalf("event workflow was not exactly-once: events=%d outbox=%d published=%d alerts=%d receipts=%d event_audit=%d alert_audit=%d acknowledgment_audit=%d camera_audit=%d actions=%d realtime=%d", eventCount, outboxCount, publishedCount, alertCount, receiptCount, eventAuditCount, alertAuditCount, acknowledgmentAuditCount, cameraAuditCount, actionCount, realtimeCount)
	}
	if _, err := auditTx.Exec(ctx, "UPDATE alerts.alert_actions SET action = 'resolve' WHERE tenant_id = $1::uuid", tenantA); err == nil {
		t.Fatal("append-only alert action trigger allowed an update")
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
