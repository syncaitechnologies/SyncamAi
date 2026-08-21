package configdelivery

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	tenantID = "11111111-1111-4111-8111-111111111111"
	siteID   = "22222222-2222-4222-8222-222222222222"
	deviceID = "33333333-3333-4333-8333-333333333333"
)

func TestMemoryRepositoryPublishesPullsAndReportsDeviceOutcome(t *testing.T) {
	repository := NewMemoryRepository([]DeviceBinding{{ID: deviceID, TenantID: tenantID, SiteID: siteID}})
	repository.now = func() time.Time { return time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC) }
	revision, err := repository.Publish(context.Background(), PublishCommand{TenantID: tenantID, SiteID: siteID, ActorID: "user-1", RequestID: "44444444-4444-4444-8444-444444444444", Payload: []byte(`{"zones":[]}`)})
	if err != nil || revision.Number != 1 || revision.ContentHash == "" {
		t.Fatalf("publish configuration: %#v, %v", revision, err)
	}
	if desired, err := repository.DesiredRevision(context.Background(), deviceID); err != nil || desired != 1 {
		t.Fatalf("desired revision: %d, %v", desired, err)
	}
	pulled, err := repository.Pull(context.Background(), deviceID, 0)
	if err != nil || pulled.Revision == nil || pulled.Revision.Number != 1 {
		t.Fatalf("pull revision: %#v, %v", pulled, err)
	}
	if unchanged, err := repository.Pull(context.Background(), deviceID, 1); err != nil || unchanged.Revision != nil {
		t.Fatalf("must not return an unchanged revision: %#v, %v", unchanged, err)
	}
	failed, err := repository.Report(context.Background(), ReportCommand{DeviceID: deviceID, Revision: 1, State: StatusFailed, ErrorMessage: "invalid local rule"})
	if err != nil || failed.State != StatusFailed || failed.AppliedAt != nil {
		t.Fatalf("report failed apply: %#v, %v", failed, err)
	}
	applied, err := repository.Report(context.Background(), ReportCommand{DeviceID: deviceID, Revision: 1, State: StatusApplied})
	if err != nil || applied.State != StatusApplied || applied.AppliedAt == nil {
		t.Fatalf("report applied config: %#v, %v", applied, err)
	}
	if _, err := repository.Report(context.Background(), ReportCommand{DeviceID: deviceID, Revision: 2, State: StatusApplied}); !errors.Is(err, ErrRevisionNotFound) {
		t.Fatalf("unknown revision must fail closed: %v", err)
	}
}

func TestMemoryRepositoryDoesNotCrossSiteOrAcceptInvalidReports(t *testing.T) {
	repository := NewMemoryRepository([]DeviceBinding{{ID: deviceID, TenantID: tenantID, SiteID: siteID}})
	if desired, err := repository.DesiredRevision(context.Background(), deviceID); err != nil || desired != 0 {
		t.Fatalf("empty desired revision: %d, %v", desired, err)
	}
	if _, err := repository.DesiredRevision(context.Background(), "44444444-4444-4444-8444-444444444444"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("unknown desired revision: %v", err)
	}
	if _, err := repository.Pull(context.Background(), "44444444-4444-4444-8444-444444444444", 0); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("unknown device pull: %v", err)
	}
	if _, err := repository.Report(context.Background(), ReportCommand{DeviceID: deviceID, Revision: 0, State: StatusApplied}); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid report must be rejected: %v", err)
	}
	if _, err := repository.Publish(context.Background(), PublishCommand{TenantID: tenantID, SiteID: siteID, Payload: []byte(`[]`)}); err == nil {
		t.Fatal("array payload must not become a configuration snapshot")
	}
}

func TestMemoryRepositoryListsTenantRevisionsAndReturnsDeviceStatus(t *testing.T) {
	repository := NewMemoryRepository([]DeviceBinding{{ID: deviceID, TenantID: tenantID, SiteID: siteID}})
	for _, command := range []PublishCommand{
		{TenantID: tenantID, SiteID: siteID, Payload: []byte(`{"zones":[1]}`)},
		{TenantID: tenantID, SiteID: siteID, Payload: []byte(`{"zones":[2]}`)},
		{TenantID: "44444444-4444-4444-8444-444444444444", SiteID: siteID, Payload: []byte(`{"zones":[3]}`)},
	} {
		if _, err := repository.Publish(context.Background(), command); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := repository.List(context.Background(), tenantID, siteID)
	if err != nil || len(listed) != 2 || listed[0].Number != 2 {
		t.Fatalf("site revisions: %#v, %v", listed, err)
	}
	all, err := repository.List(context.Background(), tenantID, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("tenant revisions: %#v, %v", all, err)
	}
	if _, err := repository.Report(context.Background(), ReportCommand{DeviceID: deviceID, Revision: 2, State: StatusApplied}); err != nil {
		t.Fatal(err)
	}
	status, err := repository.GetStatus(context.Background(), tenantID, deviceID)
	if err != nil || status.State != StatusApplied || status.AppliedAt == nil {
		t.Fatalf("status: %#v, %v", status, err)
	}
	if _, err := repository.GetStatus(context.Background(), "44444444-4444-4444-8444-444444444444", deviceID); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("cross-tenant status: %v", err)
	}
}
