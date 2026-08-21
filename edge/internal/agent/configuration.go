package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ConfigPollInterval = 30 * time.Second
	maxConfigBody      = 1 << 20
)

var (
	ErrInvalidConfiguration = errors.New("configuration revision is invalid")
	ErrConfigResponse       = errors.New("configuration response is malformed")
)

// ConfigurationRevision is the server-signed-by-transport immutable payload
// that an edge device receives. ContentHash makes accidental corruption or a
// wrong revision fail before it can replace the last accepted configuration.
type ConfigurationRevision struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	SiteID      string          `json:"site_id"`
	Number      int64           `json:"number"`
	Payload     json.RawMessage `json:"payload"`
	ContentHash string          `json:"content_hash"`
	CreatedAt   time.Time       `json:"created_at"`
}

type configEnvelope struct {
	Data ConfigurationRevision `json:"data"`
}
type configStatusEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type ConfigurationClient struct {
	endpoint   *url.URL
	deviceID   string
	httpClient *http.Client
}

func NewConfigurationClient(endpoint, deviceID string, httpClient *http.Client) (*ConfigurationClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidEndpoint
	}
	canonicalDeviceID, err := normalizeUUIDv4(deviceID)
	if err != nil {
		return nil, ErrInvalidDeviceID
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &ConfigurationClient{endpoint: parsed, deviceID: canonicalDeviceID, httpClient: httpClient}, nil
}

// Pull returns the newest immutable revision after the supplied local revision.
// A 204 response is an expected no-change state, not an error.
func (c *ConfigurationClient) Pull(ctx context.Context, afterRevision int64) (*ConfigurationRevision, error) {
	if c == nil || c.endpoint == nil || c.httpClient == nil || afterRevision < 0 {
		return nil, ErrInvalidConfiguration
	}
	path := c.endpoint.Path + "/v1/edge/devices/" + c.deviceID + "/config"
	requestURL := c.endpoint.ResolveReference(&url.URL{Path: path, RawQuery: "after_revision=" + strconv.FormatInt(afterRevision, 10)})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build configuration pull: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("pull configuration: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxConfigBody))
		return nil, &ResponseError{StatusCode: response.StatusCode}
	}
	var envelope configEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxConfigBody)).Decode(&envelope); err != nil {
		return nil, ErrConfigResponse
	}
	if err := validateConfiguration(envelope.Data); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (c *ConfigurationClient) Report(ctx context.Context, revision int64, state, errorMessage string) error {
	if c == nil || c.endpoint == nil || c.httpClient == nil || revision < 1 || (state != "applied" && state != "failed") || len(strings.TrimSpace(errorMessage)) > 512 || (state == "applied" && strings.TrimSpace(errorMessage) != "") {
		return ErrInvalidConfiguration
	}
	payload, err := json.Marshal(map[string]any{"revision": revision, "state": state, "error_message": strings.TrimSpace(errorMessage)})
	if err != nil {
		return err
	}
	path := c.endpoint.Path + "/v1/edge/devices/" + c.deviceID + "/config/status"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.ResolveReference(&url.URL{Path: path}).String(), strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("build configuration report: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	correlationID, err := newUUIDv4()
	if err != nil {
		return fmt.Errorf("generate correlation id: %w", err)
	}
	request.Header.Set("X-Correlation-Id", correlationID)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("report configuration: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxConfigBody))
		return &ResponseError{StatusCode: response.StatusCode}
	}
	var envelope configStatusEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxConfigBody)).Decode(&envelope); err != nil || len(envelope.Data) == 0 {
		return ErrConfigResponse
	}
	return nil
}

func validateConfiguration(revision ConfigurationRevision) error {
	if revision.Number < 1 || len(revision.Payload) == 0 || !json.Valid(revision.Payload) || len(revision.ContentHash) != sha256.Size*2 || revision.CreatedAt.IsZero() {
		return ErrInvalidConfiguration
	}
	var decoded any
	if err := json.Unmarshal(revision.Payload, &decoded); err != nil {
		return ErrInvalidConfiguration
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return ErrInvalidConfiguration
	}
	sum := sha256.Sum256(canonical)
	if !strings.EqualFold(revision.ContentHash, hex.EncodeToString(sum[:])) {
		return ErrInvalidConfiguration
	}
	return nil
}

// AtomicApplier replaces the local active configuration only after the full
// candidate is durable. Its contract is that an error leaves the prior active
// revision usable, which is the rollback boundary for a failed apply.
type AtomicApplier interface {
	ApplyAtomic(context.Context, ConfigurationRevision) error
}

// AtomicFileApplier stores the accepted revision in one local JSON file. It
// writes and fsyncs a sibling temporary file before renaming, so power loss or
// validation failure cannot leave a partially-written active configuration.
type AtomicFileApplier struct{ Path string }

func (a AtomicFileApplier) ApplyAtomic(ctx context.Context, revision ConfigurationRevision) error {
	if err := validateConfiguration(revision); err != nil {
		return err
	}
	if strings.TrimSpace(a.Path) == "" {
		return ErrInvalidConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(a.Path), ".configuration-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	encoded, err := json.Marshal(revision)
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("encode configuration revision: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(temporary, a.Path); err != nil {
		return fmt.Errorf("activate configuration atomically: %w", err)
	}
	return nil
}

// ConfigurationSynchronizer supports regular pull and an immediate sync when
// a heartbeat advertises a newer desired revision. A failed apply is reported
// and leaves AppliedRevision unchanged, preserving the last known-good config.
type ConfigurationSynchronizer struct {
	client  *ConfigurationClient
	applier AtomicApplier
	mu      sync.Mutex
	applied int64
}

func NewConfigurationSynchronizer(client *ConfigurationClient, applier AtomicApplier, appliedRevision int64) (*ConfigurationSynchronizer, error) {
	if client == nil || applier == nil || appliedRevision < 0 {
		return nil, ErrInvalidConfiguration
	}
	return &ConfigurationSynchronizer{client: client, applier: applier, applied: appliedRevision}, nil
}

func (s *ConfigurationSynchronizer) AppliedRevision() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied
}

func (s *ConfigurationSynchronizer) Sync(ctx context.Context) error {
	s.mu.Lock()
	after := s.applied
	s.mu.Unlock()
	revision, err := s.client.Pull(ctx, after)
	if err != nil || revision == nil {
		return err
	}
	if revision.Number <= after {
		return nil
	}
	if err := s.applier.ApplyAtomic(ctx, *revision); err != nil {
		_ = s.client.Report(ctx, revision.Number, "failed", safeConfigurationError(err))
		return err
	}
	s.mu.Lock()
	s.applied = revision.Number
	s.mu.Unlock()
	if err := s.client.Report(ctx, revision.Number, "applied", ""); err != nil {
		return err
	}
	return nil
}

// SyncIfDesired consumes the authenticated heartbeat push hint. It does not
// trust the hint as configuration data: it merely accelerates the normal mTLS
// pull and hash-verified atomic apply path.
func (s *ConfigurationSynchronizer) SyncIfDesired(ctx context.Context, desiredRevision int64) error {
	if desiredRevision < 0 {
		return ErrInvalidConfiguration
	}
	if desiredRevision <= s.AppliedRevision() {
		return nil
	}
	return s.Sync(ctx)
}

func (s *ConfigurationSynchronizer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 || interval > ConfigPollInterval {
		return ErrInvalidConfiguration
	}
	if err := s.Sync(ctx); err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = s.Sync(ctx)
		}
	}
}

func safeConfigurationError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}
