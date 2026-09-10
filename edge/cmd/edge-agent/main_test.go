package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStartupConfigRequiresSecretFileBoundaries(t *testing.T) {
	values := map[string]string{
		"SYNCAM_EDGE_API_BASE_URL":      "https://control.example",
		"SYNCAM_EDGE_DEVICE_ID":         "77777777-7777-4777-8777-777777777777",
		"SYNCAM_EDGE_FIRMWARE_VERSION":  "1.0.0",
		"SYNCAM_EDGE_STATE_DIR":         t.TempDir(),
		"SYNCAM_EDGE_CLIENT_CERT_FILE":  "client.crt",
		"SYNCAM_EDGE_CLIENT_KEY_FILE":   "client.key",
		"SYNCAM_EDGE_CA_FILE":           "root.pem",
		"SYNCAM_EDGE_RTSP_SOURCES_FILE": "sources.json",
	}
	config, err := loadStartupConfig(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.FFmpegBinary != "ffmpeg" || !filepath.IsAbs(config.Certificate) || !filepath.IsAbs(config.RTSPSources) {
		t.Fatalf("unexpected normalized config: %+v", config)
	}
	delete(values, "SYNCAM_EDGE_CLIENT_KEY_FILE")
	if _, err := loadStartupConfig(func(name string) string { return values[name] }); !errors.Is(err, errStartupConfiguration) {
		t.Fatalf("missing key path must fail: %v", err)
	}
}

func TestLoadIngestsValidatesBoundedCredentialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.json")
	valid := `{"sources":[{"id":"camera-1","url":"rtsps://camera.local/live","transport":"tcp","codec":"h264","decode_preference":"software"}]}`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	ingests, err := loadIngests(path, "ffmpeg-safe")
	if err != nil || len(ingests) != 1 {
		t.Fatalf("valid sources: count=%d err=%v", len(ingests), err)
	}

	invalid := `{"sources":[{"id":"camera-1","url":"rtsp://camera/live","codec":"h264"},{"id":"camera-1","url":"rtsp://camera/other","codec":"h264"}]}`
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIngests(path, "ffmpeg"); !errors.Is(err, errStartupSources) {
		t.Fatalf("duplicate source IDs must fail: %v", err)
	}
}

func TestLoadMTLSClientFailsClosedWithoutIdentity(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.pem")
	if _, err := loadMTLSClient(missing, missing, missing); !errors.Is(err, errStartupTLS) {
		t.Fatalf("missing identity must fail closed: %v", err)
	}
}
