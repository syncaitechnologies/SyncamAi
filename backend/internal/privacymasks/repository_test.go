package privacymasks

import (
	"context"
	"errors"
	"testing"
)

const (
	tenantID = "11111111-1111-4111-8111-111111111111"
	siteID   = "22222222-2222-4222-8222-222222222222"
	cameraID = "33333333-3333-4333-8333-333333333333"
)

func command(actor string) CreateCommand {
	return CreateCommand{TenantID: tenantID, SiteID: siteID, CameraID: cameraID, ActorID: actor, Name: "Entry privacy", Geometry: []byte(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`)}
}

func TestTwoDistinctApproversAreRequiredAndRequesterIsDenied(t *testing.T) {
	repository := NewMemoryRepository(nil)
	request, err := repository.Create(context.Background(), command("requester"))
	if err != nil || request.Status != StatusPending {
		t.Fatalf("create request: %v %#v", err, request)
	}
	if _, err := repository.Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "requester"}); !errors.Is(err, ErrRequesterCannotApprove) {
		t.Fatalf("requester approval must fail, got %v", err)
	}
	first, err := repository.Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-a"})
	if err != nil || first.Status != StatusPending || len(first.Approvals) != 1 {
		t.Fatalf("first approval: %v %#v", err, first)
	}
	replay, err := repository.Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-a"})
	if err != nil || len(replay.Approvals) != 1 {
		t.Fatalf("duplicate approval must be idempotent: %v %#v", err, replay)
	}
	second, err := repository.Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-b"})
	if err != nil || second.Status != StatusApproved || len(second.Approvals) != 2 {
		t.Fatalf("second distinct approval: %v %#v", err, second)
	}
	if _, err := repository.Approve(context.Background(), ApproveCommand{TenantID: tenantID, RequestID: request.ID, ActorID: "approver-c"}); !errors.Is(err, ErrAlreadyApproved) {
		t.Fatalf("third approval must fail, got %v", err)
	}
}

func TestCreateFailsClosedOnInvalidMetadata(t *testing.T) {
	repository := NewMemoryRepository(nil)
	for _, invalid := range []CreateCommand{
		{TenantID: tenantID, SiteID: siteID, CameraID: cameraID, ActorID: "requester", Name: "Mask", Geometry: []byte(`{"type":"LineString"}`)},
		{TenantID: tenantID, SiteID: siteID, CameraID: "invalid", ActorID: "requester", Name: "Mask", Geometry: []byte(`{"type":"Polygon"}`)},
	} {
		if _, err := repository.Create(context.Background(), invalid); err == nil {
			t.Fatal("invalid privacy-mask metadata must fail closed")
		}
	}
}

func TestGetIsTenantScopedAndReturnsCopies(t *testing.T) {
	repository := NewMemoryRepository(nil)
	created, err := repository.Create(context.Background(), command("requester"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.Get(context.Background(), tenantID, created.ID)
	if err != nil || loaded.ID != created.ID {
		t.Fatalf("get request: %v %#v", err, loaded)
	}
	loaded.Approvals = append(loaded.Approvals, Approval{ApproverID: "mutated"})
	again, err := repository.Get(context.Background(), tenantID, created.ID)
	if err != nil || len(again.Approvals) != 0 {
		t.Fatalf("repository state must not leak through copies: %v %#v", err, again)
	}
	if _, err := repository.Get(context.Background(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get must be hidden, got %v", err)
	}
}
