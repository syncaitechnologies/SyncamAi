package agent

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testDeviceID = "77777777-7777-4777-8777-777777777777"

func TestNewHeartbeatClientRejectsUntrustedEndpoint(t *testing.T) {
	for _, endpoint := range []string{"http://localhost", "https://", "https://user:pass@example.test", "https://example.test?tenant=secret"} {
		if _, err := NewHeartbeatClient(endpoint, testDeviceID, nil); !errors.Is(err, ErrInvalidEndpoint) {
			t.Fatalf("endpoint %q: expected invalid endpoint, got %v", endpoint, err)
		}
	}
	if _, err := NewHeartbeatClient("https://example.test", "not-a-device", nil); !errors.Is(err, ErrInvalidDeviceID) {
		t.Fatalf("expected invalid device id, got %v", err)
	}
	if _, err := NewMTLSHTTPClient(tls.Certificate{}, nil); !errors.Is(err, ErrInvalidClientCertificate) {
		t.Fatalf("expected invalid certificate, got %v", err)
	}
}

func TestHeartbeatClientSendsBoundedTelemetry(t *testing.T) {
	var received Telemetry
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/control/v1/edge/devices/"+testDeviceID+"/heartbeat" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Correlation-Id") == "" || r.Header.Get("X-SentinelVision-Tenant-ID") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("device request carried the wrong authentication headers")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"device":{"id":"` + testDeviceID + `","status":"active","certificate_status":"active","firmware_version":"1.2.3","store_forward_depth":7,"uptime_seconds":42,"health":{"cpu_utilization_percent":42,"gpu_utilization_percent":64,"temperature_celsius":81,"inference_latency_ms":17,"thermal_state":"warning"}},"observed_at":"2026-08-13T12:00:00Z"}}`))
	}))
	defer server.Close()

	client, err := NewHeartbeatClient(server.URL+"/control/", strings.ToUpper(testDeviceID), server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }
	health, err := NormalizeHealth(HealthSample{CPUUtilizationPercent: 42, GPUUtilizationPercent: 64, TemperatureCelsius: 81, InferenceLatencyMs: 17})
	if err != nil {
		t.Fatalf("normalize health: %v", err)
	}
	result, err := client.Send(context.Background(), Telemetry{ReportedAt: time.Time{}, UptimeSeconds: 42, StoreForwardDepth: 7, FirmwareVersion: " 1.2.3 ", Health: &health})
	if err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	if result.Device.ID != testDeviceID || result.Device.Status != "active" || result.Device.Health == nil || result.Device.Health.ThermalState != ThermalWarning || result.Replayed || received.HeartbeatID == "" || received.ReportedAt.IsZero() || received.FirmwareVersion != "1.2.3" || received.Health == nil || received.Health.ThermalState != ThermalWarning {
		t.Fatalf("unexpected heartbeat exchange: result=%+v request=%+v", result, received)
	}
}

func TestHeartbeatClientRejectsInvalidTelemetryAndResponses(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewHeartbeatClient(server.URL, testDeviceID, server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	for _, telemetry := range []Telemetry{
		{HeartbeatID: "bad", FirmwareVersion: "1"},
		{UptimeSeconds: -1, FirmwareVersion: "1"},
		{StoreForwardDepth: maxStoreForwardDepth + 1, FirmwareVersion: "1"},
		{FirmwareVersion: "1", Health: &HealthTelemetry{CPUUtilizationPercent: 101, ThermalState: ThermalNormal}},
		{FirmwareVersion: strings.Repeat("x", maxFirmwareVersionSize+1)},
	} {
		if _, err := client.Send(context.Background(), telemetry); !errors.Is(err, ErrInvalidTelemetry) {
			t.Fatalf("telemetry %+v: expected validation error, got %v", telemetry, err)
		}
	}
	result, err := client.Send(context.Background(), Telemetry{FirmwareVersion: "1"})
	if result != (HeartbeatResult{}) || err == nil {
		t.Fatalf("expected HTTP error, result=%+v err=%v", result, err)
	}
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) || responseErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 response error, got %v", err)
	}
}

func TestHeartbeatClientMarksReplayAndRejectsMalformedPayload(t *testing.T) {
	responseMode := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Idempotent-Replayed", "true")
		if responseMode == 0 {
			_, _ = w.Write([]byte(`{"data":{"device":{"id":"bad"}}}`))
			return
		}
		if responseMode == 1 {
			_, _ = w.Write([]byte(`{"data":{"device":{"id":"` + testDeviceID + `","status":"active","certificate_status":"active","health":{"cpu_utilization_percent":1,"gpu_utilization_percent":2,"temperature_celsius":80,"inference_latency_ms":3,"thermal_state":"normal"}},"observed_at":"2026-08-13T12:00:00Z"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"device":{"id":"` + testDeviceID + `","status":"active","certificate_status":"active"},"observed_at":"2026-08-13T12:00:00Z"}}`))
	}))
	defer server.Close()
	client, err := NewHeartbeatClient(server.URL, testDeviceID, server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Send(context.Background(), Telemetry{FirmwareVersion: "1"}); !errors.Is(err, ErrMalformedHeartbeat) {
		t.Fatalf("expected malformed response, got %v", err)
	}
	responseMode = 1
	if _, err := client.Send(context.Background(), Telemetry{FirmwareVersion: "1"}); !errors.Is(err, ErrMalformedHeartbeat) {
		t.Fatalf("expected invalid response health rejection, got %v", err)
	}
	responseMode = 2
	result, err := client.Send(context.Background(), Telemetry{FirmwareVersion: "1"})
	if err != nil || !result.Replayed {
		t.Fatalf("expected replayed heartbeat, result=%+v err=%v", result, err)
	}
}

func TestHeartbeatClientRunReportsAttemptsUntilCanceled(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"device":{"id":"` + testDeviceID + `","status":"active","certificate_status":"active"},"observed_at":"2026-08-13T12:00:00Z"}}`))
	}))
	defer server.Close()
	client, err := NewHeartbeatClient(server.URL, testDeviceID, server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	err = client.Run(ctx, time.Millisecond, func() Telemetry { return Telemetry{HeartbeatID: testDeviceID, FirmwareVersion: "1"} }, func(_ HeartbeatResult, sendErr error) {
		if sendErr != nil {
			t.Errorf("heartbeat attempt failed: %v", sendErr)
		}
		attempts++
		if attempts == 2 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || attempts != 2 {
		t.Fatalf("expected two attempts and cancellation, attempts=%d err=%v", attempts, err)
	}
}

func TestNewMTLSHTTPClientUsesCertificateAndTrustPool(t *testing.T) {
	certificate, clientCA := testClientCertificate(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
			t.Error("server did not receive a verified client certificate")
		}
		_, _ = w.Write([]byte(`{"data":{"device":{"id":"` + testDeviceID + `","status":"active","certificate_status":"active"},"observed_at":"2026-08-13T12:00:00Z"}}`))
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCA}
	server.StartTLS()
	defer server.Close()
	serverRoots := x509.NewCertPool()
	serverRoots.AddCert(server.Certificate())
	httpClient, err := NewMTLSHTTPClient(certificate, serverRoots)
	if err != nil {
		t.Fatalf("new mTLS client: %v", err)
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig.MinVersion != tls.VersionTLS13 || len(transport.TLSClientConfig.Certificates) != 1 {
		t.Fatalf("mTLS transport was not constrained as expected")
	}
	heartbeatClient, err := NewHeartbeatClient(server.URL, testDeviceID, httpClient)
	if err != nil {
		t.Fatalf("new heartbeat client: %v", err)
	}
	if _, err := heartbeatClient.Send(context.Background(), Telemetry{FirmwareVersion: "1"}); err != nil {
		t.Fatalf("mTLS heartbeat failed: %v", err)
	}
}

func testClientCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "syncam-test-client"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create test certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse test certificate: %v", err)
	}
	pemCertificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pemKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal test key: %v", err)
	}
	pemPrivateKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pemKey})
	clientCertificate, err := tls.X509KeyPair(pemCertificate, pemPrivateKey)
	if err != nil {
		t.Fatalf("load test key pair: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return clientCertificate, roots
}
