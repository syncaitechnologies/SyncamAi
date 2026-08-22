// Package privacymasks owns the security-governance workflow for masks.
// It never receives frames, pixels, stream credentials, or masking output.
package privacymasks

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound               = errors.New("privacy mask request not found")
	ErrRequesterCannotApprove = errors.New("privacy mask requester cannot approve")
	ErrAlreadyApproved        = errors.New("privacy mask request already fully approved")
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
)

// Approval is an immutable record of one Super Admin decision.
type Approval struct {
	ApproverID string    `json:"approver_id"`
	ApprovedAt time.Time `json:"approved_at"`
}

// Request is approval metadata for a future mask configuration. Geometry is
// configuration metadata only; no video, pixels, or executable mask exists.
type Request struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	SiteID      string          `json:"site_id"`
	CameraID    string          `json:"camera_id"`
	Name        string          `json:"name"`
	Geometry    json.RawMessage `json:"geometry"`
	Status      string          `json:"status"`
	RequestedBy string          `json:"requested_by"`
	RequestedAt time.Time       `json:"requested_at"`
	Approvals   []Approval      `json:"approvals"`
}

type CreateCommand struct {
	TenantID, SiteID, CameraID, ActorID string
	Name                                string
	Geometry                            json.RawMessage
}

type ApproveCommand struct{ TenantID, RequestID, ActorID string }

type Repository interface {
	Create(context.Context, CreateCommand) (Request, error)
	Get(context.Context, string, string) (Request, error)
	Approve(context.Context, ApproveCommand) (Request, error)
}

// MemoryRepository makes the approval semantics executable in local and HTTP
// tests. Durable immutable persistence is added in the next security slice.
type MemoryRepository struct {
	mu       sync.Mutex
	requests []Request
	now      func() time.Time
}

func NewMemoryRepository(seed []Request) *MemoryRepository {
	requests := make([]Request, len(seed))
	for index := range seed {
		requests[index] = clone(seed[index])
	}
	return &MemoryRepository{requests: requests, now: func() time.Time { return time.Now().UTC() }}
}

func (r *MemoryRepository) Create(_ context.Context, command CreateCommand) (Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validateCreate(command); err != nil {
		return Request{}, err
	}
	request := Request{ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID, CameraID: command.CameraID, Name: strings.TrimSpace(command.Name), Geometry: append(json.RawMessage(nil), command.Geometry...), Status: StatusPending, RequestedBy: command.ActorID, RequestedAt: r.now().UTC()}
	r.requests = append(r.requests, request)
	return clone(request), nil
}

func (r *MemoryRepository) Get(_ context.Context, tenantID, requestID string) (Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index := r.find(tenantID, requestID); index >= 0 {
		return clone(r.requests[index]), nil
	}
	return Request{}, ErrNotFound
}

func (r *MemoryRepository) Approve(_ context.Context, command ApproveCommand) (Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.find(command.TenantID, command.RequestID)
	if index < 0 {
		return Request{}, ErrNotFound
	}
	request := r.requests[index]
	if command.ActorID == request.RequestedBy {
		return Request{}, ErrRequesterCannotApprove
	}
	for _, approval := range request.Approvals {
		if approval.ApproverID == command.ActorID {
			return clone(request), nil
		}
	}
	if request.Status == StatusApproved {
		return Request{}, ErrAlreadyApproved
	}
	request.Approvals = append(request.Approvals, Approval{ApproverID: command.ActorID, ApprovedAt: r.now().UTC()})
	if len(request.Approvals) == 2 {
		request.Status = StatusApproved
	}
	r.requests[index] = request
	return clone(request), nil
}

func (r *MemoryRepository) find(tenantID, requestID string) int {
	for index, request := range r.requests {
		if request.TenantID == tenantID && request.ID == requestID {
			return index
		}
	}
	return -1
}

func validateCreate(command CreateCommand) error {
	for _, value := range []string{command.TenantID, command.SiteID, command.CameraID} {
		if _, err := uuid.Parse(value); err != nil {
			return errors.New("privacy mask identifiers must be UUIDs")
		}
	}
	if strings.TrimSpace(command.ActorID) == "" || len(strings.TrimSpace(command.Name)) == 0 || len(strings.TrimSpace(command.Name)) > 120 {
		return errors.New("privacy mask requester and name are required")
	}
	if len(command.Geometry) == 0 || !json.Valid(command.Geometry) {
		return errors.New("privacy mask geometry is invalid")
	}
	var geometry struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(command.Geometry, &geometry); err != nil || geometry.Type != "Polygon" {
		return errors.New("privacy mask geometry must be a polygon")
	}
	return nil
}

func clone(request Request) Request {
	request.Geometry = append(json.RawMessage(nil), request.Geometry...)
	request.Approvals = append([]Approval(nil), request.Approvals...)
	sort.Slice(request.Approvals, func(left, right int) bool {
		return request.Approvals[left].ApprovedAt.Before(request.Approvals[right].ApprovedAt)
	})
	return request
}
