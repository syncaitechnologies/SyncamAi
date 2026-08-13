// Package device owns tenant-scoped camera registration and lifecycle state.
package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCameraNotFound      = errors.New("camera not found")
	ErrSiteNotFound        = errors.New("site not found")
	ErrSerialConflict      = errors.New("camera serial conflicts with tenant state")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
	ErrVersionConflict     = errors.New("camera configuration version conflict")
	ErrLifecycleConflict   = errors.New("camera lifecycle transition is not allowed")
)

// Camera is metadata only. Stream URLs and device credentials are never returned.
type Camera struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	SiteID          string    `json:"site_id"`
	SerialNumber    string    `json:"serial_number"`
	Name            string    `json:"name"`
	GroupName       string    `json:"group_name,omitempty"`
	Tags            []string  `json:"tags"`
	LifecycleStatus string    `json:"lifecycle_status"`
	ConfigVersion   int64     `json:"config_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateCameraCommand struct {
	TenantID       string
	ActorID        string
	RequestID      string
	IdempotencyKey string
	SiteID         string
	SerialNumber   string
	Name           string
	GroupName      string
	Tags           []string
}

type CreateCameraResult struct {
	Camera   Camera
	Replayed bool
}

type UpdateCameraCommand struct {
	TenantID        string
	ActorID         string
	RequestID       string
	CameraID        string
	ExpectedVersion int64
	Name            *string
	GroupName       *string
	Tags            *[]string
	LifecycleStatus *string
}

type RetireCameraCommand struct {
	TenantID  string
	ActorID   string
	RequestID string
	CameraID  string
}

type Repository interface {
	List(context.Context, string, string) ([]Camera, error)
	Get(context.Context, string, string) (Camera, error)
	Create(context.Context, CreateCameraCommand) (CreateCameraResult, error)
	Update(context.Context, UpdateCameraCommand) (Camera, error)
	Retire(context.Context, RetireCameraCommand) (Camera, error)
}

type memoryReplay struct {
	hash   string
	camera Camera
}

// MemoryRepository provides deterministic behavior for unit and HTTP tests.
type MemoryRepository struct {
	mu          sync.Mutex
	cameras     []Camera
	idempotency map[string]memoryReplay
	now         func() time.Time
}

func NewMemoryRepository(cameras []Camera) *MemoryRepository {
	seed := append([]Camera(nil), cameras...)
	for index := range seed {
		seed[index].Tags = append([]string(nil), seed[index].Tags...)
	}
	return &MemoryRepository{cameras: seed, idempotency: make(map[string]memoryReplay), now: func() time.Time { return time.Now().UTC() }}
}

func (r *MemoryRepository) List(_ context.Context, tenantID, siteID string) ([]Camera, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Camera, 0)
	for _, camera := range r.cameras {
		if camera.TenantID == tenantID && camera.LifecycleStatus != "retired" && (siteID == "" || camera.SiteID == siteID) {
			result = append(result, cloneCamera(camera))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *MemoryRepository) Get(_ context.Context, tenantID, cameraID string) (Camera, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.find(tenantID, cameraID)
	if index < 0 || r.cameras[index].LifecycleStatus == "retired" {
		return Camera{}, ErrCameraNotFound
	}
	return cloneCamera(r.cameras[index]), nil
}

func (r *MemoryRepository) Create(_ context.Context, command CreateCameraCommand) (CreateCameraResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hash, err := hashCreateCamera(command)
	if err != nil {
		return CreateCameraResult{}, err
	}
	key := command.TenantID + ":" + command.IdempotencyKey
	if replay, ok := r.idempotency[key]; ok {
		if replay.hash != hash {
			return CreateCameraResult{}, ErrIdempotencyConflict
		}
		return CreateCameraResult{Camera: cloneCamera(replay.camera), Replayed: true}, nil
	}
	serial := normalizeSerial(command.SerialNumber)
	for _, camera := range r.cameras {
		if camera.TenantID == command.TenantID && normalizeSerial(camera.SerialNumber) == serial {
			return CreateCameraResult{}, ErrSerialConflict
		}
	}
	now := r.now()
	camera := Camera{
		ID: uuid.NewString(), TenantID: command.TenantID, SiteID: command.SiteID,
		SerialNumber: serial, Name: strings.TrimSpace(command.Name), GroupName: strings.TrimSpace(command.GroupName),
		Tags: normalizeTags(command.Tags), LifecycleStatus: "pending", ConfigVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	r.cameras = append(r.cameras, camera)
	r.idempotency[key] = memoryReplay{hash: hash, camera: cloneCamera(camera)}
	return CreateCameraResult{Camera: cloneCamera(camera)}, nil
}

func (r *MemoryRepository) Update(_ context.Context, command UpdateCameraCommand) (Camera, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.find(command.TenantID, command.CameraID)
	if index < 0 || r.cameras[index].LifecycleStatus == "retired" {
		return Camera{}, ErrCameraNotFound
	}
	current := r.cameras[index]
	if current.ConfigVersion != command.ExpectedVersion {
		return Camera{}, ErrVersionConflict
	}
	next, changed, err := applyUpdate(current, command)
	if err != nil {
		return Camera{}, err
	}
	if changed {
		next.ConfigVersion++
		next.UpdatedAt = r.now()
		r.cameras[index] = next
	}
	return cloneCamera(next), nil
}

func (r *MemoryRepository) Retire(_ context.Context, command RetireCameraCommand) (Camera, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.find(command.TenantID, command.CameraID)
	if index < 0 {
		return Camera{}, ErrCameraNotFound
	}
	camera := r.cameras[index]
	if camera.LifecycleStatus == "retired" {
		return cloneCamera(camera), nil
	}
	camera.LifecycleStatus = "retired"
	camera.ConfigVersion++
	camera.UpdatedAt = r.now()
	r.cameras[index] = camera
	return cloneCamera(camera), nil
}

func (r *MemoryRepository) find(tenantID, cameraID string) int {
	for index, camera := range r.cameras {
		if camera.TenantID == tenantID && camera.ID == cameraID {
			return index
		}
	}
	return -1
}

func applyUpdate(current Camera, command UpdateCameraCommand) (Camera, bool, error) {
	next := cloneCamera(current)
	if command.Name != nil {
		next.Name = strings.TrimSpace(*command.Name)
	}
	if command.GroupName != nil {
		next.GroupName = strings.TrimSpace(*command.GroupName)
	}
	if command.Tags != nil {
		next.Tags = normalizeTags(*command.Tags)
	}
	if command.LifecycleStatus != nil {
		status := strings.TrimSpace(*command.LifecycleStatus)
		if !canTransition(current.LifecycleStatus, status) {
			return Camera{}, false, ErrLifecycleConflict
		}
		next.LifecycleStatus = status
	}
	return next, !sameMutableState(current, next), nil
}

func canTransition(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case "pending":
		return to == "active" || to == "retired"
	case "active":
		return to == "offline" || to == "retired"
	case "offline":
		return to == "active" || to == "retired"
	default:
		return false
	}
}

func sameMutableState(left, right Camera) bool {
	if left.Name != right.Name || left.GroupName != right.GroupName || left.LifecycleStatus != right.LifecycleStatus || len(left.Tags) != len(right.Tags) {
		return false
	}
	for index := range left.Tags {
		if left.Tags[index] != right.Tags[index] {
			return false
		}
	}
	return true
}

func hashCreateCamera(command CreateCameraCommand) (string, error) {
	payload, err := json.Marshal(struct {
		SiteID       string   `json:"site_id"`
		SerialNumber string   `json:"serial_number"`
		Name         string   `json:"name"`
		GroupName    string   `json:"group_name"`
		Tags         []string `json:"tags"`
	}{command.SiteID, normalizeSerial(command.SerialNumber), strings.TrimSpace(command.Name), strings.TrimSpace(command.GroupName), normalizeTags(command.Tags)})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:]), nil
}

func normalizeSerial(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }

func normalizeTags(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := unique[value]; exists {
			continue
		}
		unique[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneCamera(camera Camera) Camera {
	camera.Tags = append([]string(nil), camera.Tags...)
	return camera
}
