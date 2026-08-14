package device

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

const edgeDeviceA = "77777777-7777-4777-8777-777777777777"

func TestEffectiveDeviceStatusUsesCanonicalNinetySecondThreshold(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	activated := now.Add(-DeviceOfflineAfter)
	device := EdgeDevice{Status: "active", ActivatedAt: &activated}
	if got := EffectiveDeviceStatus(device, now); got != "active" {
		t.Fatalf("exactly 90 seconds is not older than threshold: %s", got)
	}
	observed := now.Add(time.Nanosecond)
	if got := EffectiveDeviceStatus(device, observed); got != "offline" {
		t.Fatalf("device older than threshold must be offline: %s", got)
	}
	device.Status = "pending"
	if got := EffectiveDeviceStatus(device, observed); got != "pending" {
		t.Fatalf("pending state must not be rewritten: %s", got)
	}
	device.Status = "retired"
	if got := EffectiveDeviceStatus(device, observed); got != "retired" {
		t.Fatalf("retired state must not be rewritten: %s", got)
	}
}

func TestMemoryStatusRepositoryFiltersFleetAndRecordsIdempotentHeartbeat(t *testing.T) {
	fixed := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	stale := fixed.Add(-DeviceOfflineAfter - time.Second)
	repository := NewMemoryStatusRepository([]EdgeDevice{
		{ID: edgeDeviceA, TenantID: tenantA, SiteID: siteA, Status: "active", CertificateStatus: "active", ActivatedAt: &stale},
		{ID: "88888888-8888-4888-8888-888888888888", TenantID: tenantA, SiteID: siteB, Status: "pending", CertificateStatus: "pending"},
		{ID: "99999999-9999-4999-8999-999999999999", TenantID: tenantB, SiteID: siteA, Status: "active", CertificateStatus: "active"},
	})
	repository.now = func() time.Time { return fixed }
	listed, err := repository.ListDevices(context.Background(), tenantA, siteA, fixed)
	if err != nil || len(listed) != 1 || listed[0].Status != "offline" {
		t.Fatalf("unexpected fleet list: %+v %v", listed, err)
	}
	command := HeartbeatCommand{
		DeviceID: edgeDeviceA, HeartbeatID: "66666666-6666-4666-8666-666666666666",
		ReportedAt: fixed.Add(-time.Second), UptimeSeconds: 42, StoreForwardDepth: 7, FirmwareVersion: " 1.2.3 ",
	}
	result, err := repository.RecordHeartbeat(context.Background(), command)
	if err != nil || result.Replayed || result.Device.Status != "active" || result.Device.FirmwareVersion != "1.2.3" || result.Device.StoreForwardDepth != 7 || result.Device.UptimeSeconds != 42 || result.Device.LastHeartbeat == nil || *result.Device.LastHeartbeat != fixed {
		t.Fatalf("unexpected heartbeat: %+v %v", result, err)
	}
	replayed, err := repository.RecordHeartbeat(context.Background(), command)
	if err != nil || !replayed.Replayed || replayed.ObservedAt != fixed {
		t.Fatalf("unexpected replay: %+v %v", replayed, err)
	}
	command.FirmwareVersion = "different"
	if _, err := repository.RecordHeartbeat(context.Background(), command); !errors.Is(err, ErrHeartbeatConflict) {
		t.Fatalf("expected heartbeat conflict, got %v", err)
	}
	if _, err := repository.RecordHeartbeat(context.Background(), HeartbeatCommand{DeviceID: "missing", HeartbeatID: command.HeartbeatID}); !errors.Is(err, ErrDeviceUnauthorized) {
		t.Fatalf("expected unknown device rejection, got %v", err)
	}
}

func TestMTLSDeviceVerifierRequiresVerifiedCurrentUUIDCertificate(t *testing.T) {
	fixed := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	verifier := MTLSDeviceVerifier{Now: func() time.Time { return fixed }}
	request := httptest.NewRequest("POST", "/", nil)
	if _, err := verifier.VerifyDevice(request); !errors.Is(err, ErrDeviceUnauthorized) {
		t.Fatalf("missing TLS must fail: %v", err)
	}
	certificate := &x509.Certificate{Subject: pkix.Name{CommonName: edgeDeviceA}, NotBefore: fixed.Add(-time.Hour), NotAfter: fixed.Add(time.Hour)}
	request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{certificate}}}
	if got, err := verifier.VerifyDevice(request); err != nil || got != edgeDeviceA {
		t.Fatalf("unexpected verified identity: %s %v", got, err)
	}
	certificate.NotAfter = fixed
	if _, err := verifier.VerifyDevice(request); !errors.Is(err, ErrDeviceUnauthorized) {
		t.Fatalf("expired certificate must fail: %v", err)
	}
	certificate.NotAfter = fixed.Add(time.Hour)
	certificate.Subject.CommonName = "not-a-device"
	if _, err := verifier.VerifyDevice(request); !errors.Is(err, ErrDeviceUnauthorized) {
		t.Fatalf("invalid CN must fail: %v", err)
	}
}
