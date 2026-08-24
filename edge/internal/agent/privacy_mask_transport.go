package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxPrivacyReleaseBody = 128 << 10

var ErrInvalidPrivacyMaskTransport = errors.New("privacy mask release transport is invalid")

// PrivacyMaskReleaseClient is a dedicated HTTPS/mTLS transport. It never uses
// generic configuration routes and carries only bounded release metadata.
type PrivacyMaskReleaseClient struct {
	endpoint   *url.URL
	deviceID   string
	httpClient *http.Client
}
type privacyMaskReleaseEnvelope struct {
	Data PrivacyMaskReleaseManifest `json:"data"`
}
type privacyMaskReleaseStatusEnvelope struct {
	Data json.RawMessage `json:"data"`
}

func NewPrivacyMaskReleaseClient(endpoint, deviceID string, httpClient *http.Client) (*PrivacyMaskReleaseClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidPrivacyMaskTransport
	}
	canonical, err := normalizeUUIDv4(deviceID)
	if err != nil {
		return nil, ErrInvalidPrivacyMaskTransport
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &PrivacyMaskReleaseClient{endpoint: parsed, deviceID: canonical, httpClient: httpClient}, nil
}

func (c *PrivacyMaskReleaseClient) Pull(ctx context.Context, afterVersion int64) (*PrivacyMaskReleaseManifest, error) {
	if c == nil || c.endpoint == nil || c.httpClient == nil || afterVersion < 0 {
		return nil, ErrInvalidPrivacyMaskTransport
	}
	path := c.endpoint.Path + "/v1/edge/devices/" + c.deviceID + "/privacy-mask-release"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint.ResolveReference(&url.URL{Path: path, RawQuery: "after_version=" + strconv.FormatInt(afterVersion, 10)}).String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build privacy release pull: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("pull privacy release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPrivacyReleaseBody))
		return nil, &ResponseError{StatusCode: response.StatusCode}
	}
	var envelope privacyMaskReleaseEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxPrivacyReleaseBody)).Decode(&envelope); err != nil {
		return nil, ErrInvalidPrivacyMaskTransport
	}
	if _, err := uuid.Parse(envelope.Data.ReleaseID); err != nil || envelope.Data.DeviceID != c.deviceID || envelope.Data.Version < 1 {
		return nil, ErrInvalidPrivacyMaskTransport
	}
	return &envelope.Data, nil
}

func (c *PrivacyMaskReleaseClient) ReportPrivacyMaskRelease(ctx context.Context, status PrivacyMaskReleaseStatus) error {
	if c == nil || c.endpoint == nil || c.httpClient == nil || status.DeviceID != c.deviceID || status.Version < 1 || (status.State != PrivacyMaskReleaseAccepted && status.State != PrivacyMaskReleaseFailed) || (status.State == PrivacyMaskReleaseAccepted && status.ErrorCode != "") || (status.State == PrivacyMaskReleaseFailed && status.ErrorCode != "verification_failed" && status.ErrorCode != "stale_release" && status.ErrorCode != "apply_failed") {
		return ErrInvalidPrivacyMaskTransport
	}
	payload, err := json.Marshal(map[string]any{"release_id": status.ReleaseID, "version": status.Version, "state": status.State, "error_code": status.ErrorCode})
	if err != nil {
		return err
	}
	path := c.endpoint.Path + "/v1/edge/devices/" + c.deviceID + "/privacy-mask-release/status"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.ResolveReference(&url.URL{Path: path}).String(), strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("build privacy release report: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	correlation, err := newUUIDv4()
	if err != nil {
		return err
	}
	request.Header.Set("X-Correlation-Id", correlation)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("report privacy release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPrivacyReleaseBody))
		return &ResponseError{StatusCode: response.StatusCode}
	}
	var envelope privacyMaskReleaseStatusEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxPrivacyReleaseBody)).Decode(&envelope); err != nil || len(envelope.Data) == 0 {
		return ErrInvalidPrivacyMaskTransport
	}
	return nil
}
