package agent

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	HeartbeatInterval      = 30 * time.Second
	maxHeartbeatBody       = 1 << 20
	maxFirmwareVersionSize = 128
	maxUptimeSeconds       = int64(3155760000)
	maxStoreForwardDepth   = int64(1000000000)
)

var (
	ErrInvalidEndpoint          = errors.New("heartbeat endpoint is invalid")
	ErrInvalidDeviceID          = errors.New("device id must be a UUIDv4")
	ErrInvalidTelemetry         = errors.New("heartbeat telemetry is invalid")
	ErrInvalidClientCertificate = errors.New("client certificate is invalid")
	ErrMalformedHeartbeat       = errors.New("heartbeat response is malformed")
)

// Telemetry is the bounded operational snapshot sent by an edge agent. A
// caller may supply HeartbeatID to retry the same logical heartbeat; an empty
// value is replaced with a fresh UUIDv4 by Send.
type Telemetry struct {
	HeartbeatID       string           `json:"heartbeat_id,omitempty"`
	ReportedAt        time.Time        `json:"reported_at"`
	UptimeSeconds     int64            `json:"uptime_seconds"`
	StoreForwardDepth int64            `json:"store_forward_depth"`
	FirmwareVersion   string           `json:"firmware_version"`
	Health            *HealthTelemetry `json:"health,omitempty"`
}

type DeviceStatus struct {
	ID                string           `json:"id"`
	Status            string           `json:"status"`
	CertificateStatus string           `json:"certificate_status"`
	FirmwareVersion   string           `json:"firmware_version,omitempty"`
	StoreForwardDepth int64            `json:"store_forward_depth"`
	UptimeSeconds     int64            `json:"uptime_seconds"`
	LastHeartbeat     time.Time        `json:"last_heartbeat"`
	Health            *HealthTelemetry `json:"health,omitempty"`
}

type HeartbeatResult struct {
	Device     DeviceStatus `json:"device"`
	ObservedAt time.Time    `json:"observed_at"`
	Replayed   bool         `json:"-"`
}

type heartbeatEnvelope struct {
	Data HeartbeatResult `json:"data"`
}

// ResponseError reports a non-success HTTP response without returning server
// response bodies that could contain tenant or operational data.
type ResponseError struct {
	StatusCode int
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("heartbeat endpoint returned HTTP %d", e.StatusCode)
}

// HeartbeatClient sends telemetry over a caller-supplied HTTP client. The
// client should use NewMTLSHTTPClient so no user OIDC or tenant header is sent
// on the device-authenticated route.
type HeartbeatClient struct {
	endpoint   *url.URL
	deviceID   string
	httpClient *http.Client
	now        func() time.Time
}

func NewHeartbeatClient(endpoint, deviceID string, httpClient *http.Client) (*HeartbeatClient, error) {
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
	return &HeartbeatClient{endpoint: parsed, deviceID: canonicalDeviceID, httpClient: httpClient, now: func() time.Time { return time.Now().UTC() }}, nil
}

// NewMTLSHTTPClient creates a TLS 1.3 client without persisting certificate
// material. Callers load certificates from the edge trust boundary and keep
// private keys outside application payloads and logs.
func NewMTLSHTTPClient(certificate tls.Certificate, roots *x509.CertPool) (*http.Client, error) {
	if len(certificate.Certificate) == 0 || certificate.PrivateKey == nil || roots == nil {
		return nil, ErrInvalidClientCertificate
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
			RootCAs:      roots,
		}},
	}, nil
}

func (c *HeartbeatClient) Send(ctx context.Context, telemetry Telemetry) (HeartbeatResult, error) {
	if c == nil || c.endpoint == nil || c.httpClient == nil {
		return HeartbeatResult{}, ErrInvalidEndpoint
	}
	normalized, err := c.normalizeTelemetry(telemetry)
	if err != nil {
		return HeartbeatResult{}, err
	}
	body, err := json.Marshal(normalized)
	if err != nil {
		return HeartbeatResult{}, fmt.Errorf("encode heartbeat: %w", err)
	}
	path := c.endpoint.Path + "/v1/edge/devices/" + c.deviceID + "/heartbeat"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.ResolveReference(&url.URL{Path: path}).String(), strings.NewReader(string(body)))
	if err != nil {
		return HeartbeatResult{}, fmt.Errorf("build heartbeat request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	correlationID, err := newUUIDv4()
	if err != nil {
		return HeartbeatResult{}, fmt.Errorf("generate correlation id: %w", err)
	}
	request.Header.Set("X-Correlation-Id", correlationID)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return HeartbeatResult{}, fmt.Errorf("send heartbeat: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxHeartbeatBody))
		return HeartbeatResult{}, &ResponseError{StatusCode: response.StatusCode}
	}
	var envelope heartbeatEnvelope
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxHeartbeatBody))
	if err := decoder.Decode(&envelope); err != nil {
		return HeartbeatResult{}, fmt.Errorf("decode heartbeat response: %w", ErrMalformedHeartbeat)
	}
	if err := validateHeartbeatResult(c.deviceID, envelope.Data); err != nil {
		return HeartbeatResult{}, err
	}
	envelope.Data.Replayed = response.Header.Get("Idempotent-Replayed") == "true"
	return envelope.Data, nil
}

// Run sends an immediate heartbeat and then sends snapshots at most every 30
// seconds. A failed attempt is reported and does not stop later attempts;
// cancellation is the only normal way to stop the loop.
func (c *HeartbeatClient) Run(ctx context.Context, interval time.Duration, snapshot func() Telemetry, report func(HeartbeatResult, error)) error {
	if interval <= 0 || interval > HeartbeatInterval || snapshot == nil || report == nil {
		return ErrInvalidTelemetry
	}
	send := func() {
		telemetry := snapshot()
		telemetry.HeartbeatID = ""
		result, err := c.Send(ctx, telemetry)
		report(result, err)
	}
	send()
	if err := ctx.Err(); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			send()
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
}

func (c *HeartbeatClient) normalizeTelemetry(telemetry Telemetry) (Telemetry, error) {
	if telemetry.HeartbeatID == "" {
		id, err := newUUIDv4()
		if err != nil {
			return Telemetry{}, err
		}
		telemetry.HeartbeatID = id
	} else if id, err := normalizeUUIDv4(telemetry.HeartbeatID); err != nil {
		return Telemetry{}, ErrInvalidTelemetry
	} else {
		telemetry.HeartbeatID = id
	}
	if telemetry.ReportedAt.IsZero() {
		telemetry.ReportedAt = c.now().UTC()
	} else {
		telemetry.ReportedAt = telemetry.ReportedAt.UTC()
	}
	if telemetry.ReportedAt.After(c.now().UTC().Add(5*time.Minute)) || telemetry.UptimeSeconds < 0 || telemetry.UptimeSeconds > maxUptimeSeconds || telemetry.StoreForwardDepth < 0 || telemetry.StoreForwardDepth > maxStoreForwardDepth || (telemetry.Health != nil && !validHealthTelemetry(*telemetry.Health)) {
		return Telemetry{}, ErrInvalidTelemetry
	}
	telemetry.FirmwareVersion = strings.TrimSpace(telemetry.FirmwareVersion)
	if telemetry.FirmwareVersion == "" || len(telemetry.FirmwareVersion) > maxFirmwareVersionSize {
		return Telemetry{}, ErrInvalidTelemetry
	}
	return telemetry, nil
}

func validateHeartbeatResult(deviceID string, result HeartbeatResult) error {
	if result.Device.ID != deviceID || result.Device.Status == "" || result.Device.CertificateStatus == "" || result.ObservedAt.IsZero() || (result.Device.Health != nil && !validHealthTelemetry(*result.Device.Health)) {
		return ErrMalformedHeartbeat
	}
	return nil
}

func newUUIDv4() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return formatUUID(bytes), nil
}

func normalizeUUIDv4(value string) (string, error) {
	compact := strings.ReplaceAll(strings.TrimSpace(value), "-", "")
	if len(compact) != 32 {
		return "", ErrInvalidDeviceID
	}
	bytes, err := hex.DecodeString(compact)
	if err != nil || len(bytes) != 16 || bytes[6]>>4 != 4 || bytes[8]&0xc0 != 0x80 {
		return "", ErrInvalidDeviceID
	}
	return formatUUID(bytes), nil
}

func formatUUID(bytes []byte) string {
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(bytes[0:4]), hex.EncodeToString(bytes[4:6]), hex.EncodeToString(bytes[6:8]), hex.EncodeToString(bytes[8:10]), hex.EncodeToString(bytes[10:16]))
}
