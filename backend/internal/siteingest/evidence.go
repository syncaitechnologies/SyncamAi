package siteingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MaxSnapshotBytes bounds one uploaded JPEG snapshot.
const MaxSnapshotBytes = 512 << 10

// SnapshotRefPrefix marks evidence references that point at stored snapshots.
const SnapshotRefPrefix = "snapshot:"

var (
	ErrEvidenceNotFound = errors.New("evidence not found")
	ErrInvalidSnapshot  = errors.New("snapshot must be a JPEG image")
)

// EvidenceStore keeps alert snapshots. References are tenant-prefixed object
// paths, so a reference from one tenant can never resolve in another.
type EvidenceStore interface {
	Put(ctx context.Context, tenantID, siteID string, jpeg []byte) (string, error)
	Get(ctx context.Context, tenantID, ref string) ([]byte, error)
	// PutReference replaces a camera's latest reference frame, used to draw zones.
	PutReference(ctx context.Context, tenantID, siteID, cameraID string, jpeg []byte) error
	GetReference(ctx context.Context, tenantID, siteID, cameraID string) ([]byte, error)
}

func referencePath(tenantID, siteID, cameraID string) string {
	return tenantID + "/" + siteID + "/reference/" + cameraID + ".jpg"
}

// IsJPEG checks the JPEG start-of-image marker.
func IsJPEG(data []byte) bool {
	return len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
}

func snapshotPath(tenantID, siteID string, now time.Time) string {
	return fmt.Sprintf("%s/%s/%s/%s.jpg", tenantID, siteID, now.UTC().Format("2006/01/02"), uuid.NewString())
}

// objectPathForTenant validates a reference and returns its object path.
func objectPathForTenant(tenantID, ref string) (string, error) {
	if !strings.HasPrefix(ref, SnapshotRefPrefix) {
		return "", ErrEvidenceNotFound
	}
	path := strings.TrimPrefix(ref, SnapshotRefPrefix)
	if !strings.HasPrefix(path, tenantID+"/") || strings.Contains(path, "..") || !strings.HasSuffix(path, ".jpg") {
		return "", ErrEvidenceNotFound
	}
	return path, nil
}

// SupabaseEvidenceStore stores snapshots in a private Supabase Storage bucket
// using a server-only secret key. Browsers never receive the key or bucket URL.
type SupabaseEvidenceStore struct {
	BaseURL   string
	SecretKey string
	Bucket    string
	Client    *http.Client
	Now       func() time.Time
}

func (s *SupabaseEvidenceStore) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (s *SupabaseEvidenceStore) objectURL(path string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return strings.TrimRight(s.BaseURL, "/") + "/storage/v1/object/" + url.PathEscape(s.Bucket) + "/" + strings.Join(segments, "/")
}

func (s *SupabaseEvidenceStore) Put(ctx context.Context, tenantID, siteID string, jpeg []byte) (string, error) {
	if !IsJPEG(jpeg) || len(jpeg) > MaxSnapshotBytes {
		return "", ErrInvalidSnapshot
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	path := snapshotPath(tenantID, siteID, now)
	if err := s.upload(ctx, path, jpeg, false); err != nil {
		return "", err
	}
	return SnapshotRefPrefix + path, nil
}

func (s *SupabaseEvidenceStore) upload(ctx context.Context, path string, jpeg []byte, upsert bool) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.objectURL(path), bytes.NewReader(jpeg))
	if err != nil {
		return err
	}
	s.authorize(request)
	request.Header.Set("Content-Type", "image/jpeg")
	request.Header.Set("x-upsert", fmt.Sprintf("%t", upsert))
	response, err := s.client().Do(request)
	if err != nil {
		return fmt.Errorf("upload snapshot: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("upload snapshot: storage returned %d", response.StatusCode)
	}
	return nil
}

func (s *SupabaseEvidenceStore) PutReference(ctx context.Context, tenantID, siteID, cameraID string, jpeg []byte) error {
	if !IsJPEG(jpeg) || len(jpeg) > MaxSnapshotBytes {
		return ErrInvalidSnapshot
	}
	return s.upload(ctx, referencePath(tenantID, siteID, cameraID), jpeg, true)
}

func (s *SupabaseEvidenceStore) GetReference(ctx context.Context, tenantID, siteID, cameraID string) ([]byte, error) {
	return s.download(ctx, referencePath(tenantID, siteID, cameraID))
}

func (s *SupabaseEvidenceStore) Get(ctx context.Context, tenantID, ref string) ([]byte, error) {
	path, err := objectPathForTenant(tenantID, ref)
	if err != nil {
		return nil, err
	}
	return s.download(ctx, path)
}

func (s *SupabaseEvidenceStore) download(ctx context.Context, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(path), nil)
	if err != nil {
		return nil, err
	}
	s.authorize(request)
	response, err := s.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("download snapshot: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusBadRequest {
		return nil, ErrEvidenceNotFound
	}
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("download snapshot: storage returned %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxSnapshotBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	if len(data) > MaxSnapshotBytes {
		return nil, ErrInvalidSnapshot
	}
	return data, nil
}

func (s *SupabaseEvidenceStore) authorize(request *http.Request) {
	request.Header.Set("apikey", s.SecretKey)
	request.Header.Set("Authorization", "Bearer "+s.SecretKey)
}

// MemoryEvidenceStore keeps snapshots in memory for tests and local runs.
type MemoryEvidenceStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemoryEvidenceStore() *MemoryEvidenceStore {
	return &MemoryEvidenceStore{objects: map[string][]byte{}}
}

func (s *MemoryEvidenceStore) Put(_ context.Context, tenantID, siteID string, jpeg []byte) (string, error) {
	if !IsJPEG(jpeg) || len(jpeg) > MaxSnapshotBytes {
		return "", ErrInvalidSnapshot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := snapshotPath(tenantID, siteID, time.Now())
	s.objects[path] = append([]byte(nil), jpeg...)
	return SnapshotRefPrefix + path, nil
}

func (s *MemoryEvidenceStore) Get(_ context.Context, tenantID, ref string) ([]byte, error) {
	path, err := objectPathForTenant(tenantID, ref)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[path]
	if !ok {
		return nil, ErrEvidenceNotFound
	}
	return append([]byte(nil), data...), nil
}

func (s *MemoryEvidenceStore) PutReference(_ context.Context, tenantID, siteID, cameraID string, jpeg []byte) error {
	if !IsJPEG(jpeg) || len(jpeg) > MaxSnapshotBytes {
		return ErrInvalidSnapshot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[referencePath(tenantID, siteID, cameraID)] = append([]byte(nil), jpeg...)
	return nil
}

func (s *MemoryEvidenceStore) GetReference(_ context.Context, tenantID, siteID, cameraID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[referencePath(tenantID, siteID, cameraID)]
	if !ok {
		return nil, ErrEvidenceNotFound
	}
	return append([]byte(nil), data...), nil
}
