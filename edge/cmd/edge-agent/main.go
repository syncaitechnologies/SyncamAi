// Command edge-agent composes the credential-safe edge control runtime.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/syncaitechnologies/SyncamAi/edge/internal/agent"
)

const (
	maxRTSPSourcesFile = 128 << 10
	maxRTSPSources     = 64
	spoolMaxBytes      = int64(1 << 30)
	spoolMaxItemBytes  = int64(16 << 20)
)

var (
	errStartupConfiguration = errors.New("edge startup configuration is invalid")
	errStartupTLS           = errors.New("edge TLS identity is invalid")
	errStartupSources       = errors.New("edge RTSP source configuration is invalid")
)

type startupConfig struct {
	APIBaseURL     string
	DeviceID       string
	Firmware       string
	StateDirectory string
	Certificate    string
	PrivateKey     string
	RootCA         string
	RTSPSources    string
	FFmpegBinary   string
}

type sourceFile struct {
	Sources []sourceConfig `json:"sources"`
}

type sourceConfig struct {
	ID                string   `json:"id"`
	URL               string   `json:"url"`
	Transport         string   `json:"transport,omitempty"`
	Codec             string   `json:"codec"`
	DecodePreference  string   `json:"decode_preference,omitempty"`
	AvailableDecoders []string `json:"available_decoders,omitempty"`
}

type runtimeLogger struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func main() {
	logger := &runtimeLogger{encoder: json.NewEncoder(os.Stdout)}
	config, err := loadStartupConfig(os.Getenv)
	if err != nil {
		logger.startupFailure("configuration")
		os.Exit(1)
	}
	runtime, err := buildRuntime(config)
	if err != nil {
		reason := "runtime"
		if errors.Is(err, errStartupTLS) {
			reason = "tls_identity"
		} else if errors.Is(err, errStartupSources) {
			reason = "rtsp_sources"
		}
		logger.startupFailure(reason)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Run(ctx, logger.event); err != nil && !errors.Is(err, context.Canceled) {
		logger.startupFailure("runtime_stopped")
		os.Exit(1)
	}
}

func loadStartupConfig(getenv func(string) string) (startupConfig, error) {
	if getenv == nil {
		return startupConfig{}, errStartupConfiguration
	}
	config := startupConfig{
		APIBaseURL:     strings.TrimSpace(getenv("SYNCAM_EDGE_API_BASE_URL")),
		DeviceID:       strings.TrimSpace(getenv("SYNCAM_EDGE_DEVICE_ID")),
		Firmware:       strings.TrimSpace(getenv("SYNCAM_EDGE_FIRMWARE_VERSION")),
		StateDirectory: strings.TrimSpace(getenv("SYNCAM_EDGE_STATE_DIR")),
		Certificate:    strings.TrimSpace(getenv("SYNCAM_EDGE_CLIENT_CERT_FILE")),
		PrivateKey:     strings.TrimSpace(getenv("SYNCAM_EDGE_CLIENT_KEY_FILE")),
		RootCA:         strings.TrimSpace(getenv("SYNCAM_EDGE_CA_FILE")),
		RTSPSources:    strings.TrimSpace(getenv("SYNCAM_EDGE_RTSP_SOURCES_FILE")),
		FFmpegBinary:   strings.TrimSpace(getenv("SYNCAM_EDGE_FFMPEG_BINARY")),
	}
	if config.APIBaseURL == "" || config.DeviceID == "" || config.Firmware == "" || config.StateDirectory == "" || config.Certificate == "" || config.PrivateKey == "" || config.RootCA == "" || config.RTSPSources == "" {
		return startupConfig{}, errStartupConfiguration
	}
	if config.FFmpegBinary == "" {
		config.FFmpegBinary = "ffmpeg"
	}
	var err error
	for _, target := range []*string{&config.StateDirectory, &config.Certificate, &config.PrivateKey, &config.RootCA, &config.RTSPSources} {
		*target, err = filepath.Abs(*target)
		if err != nil {
			return startupConfig{}, errStartupConfiguration
		}
	}
	return config, nil
}

func buildRuntime(config startupConfig) (*agent.EdgeRuntime, error) {
	httpClient, err := loadMTLSClient(config.Certificate, config.PrivateKey, config.RootCA)
	if err != nil {
		return nil, err
	}
	heartbeat, err := agent.NewHeartbeatClient(config.APIBaseURL, config.DeviceID, httpClient)
	if err != nil {
		return nil, errStartupConfiguration
	}
	configuration, err := agent.NewConfigurationClient(config.APIBaseURL, config.DeviceID, httpClient)
	if err != nil {
		return nil, errStartupConfiguration
	}
	synchronizer, err := agent.NewConfigurationSynchronizer(configuration, agent.AtomicFileApplier{Path: filepath.Join(config.StateDirectory, "configuration", "active.json")}, 0)
	if err != nil {
		return nil, errStartupConfiguration
	}
	spool, err := agent.NewDurableSpool(filepath.Join(config.StateDirectory, "spool"), spoolMaxBytes, spoolMaxItemBytes)
	if err != nil {
		return nil, errStartupConfiguration
	}
	ingests, err := loadIngests(config.RTSPSources, config.FFmpegBinary)
	if err != nil {
		return nil, err
	}
	return agent.NewEdgeRuntime(agent.EdgeRuntimeConfig{
		FirmwareVersion:       config.Firmware,
		HeartbeatInterval:     agent.HeartbeatInterval,
		ConfigurationInterval: agent.ConfigPollInterval,
	}, heartbeat, synchronizer, spool, ingests...)
}

func loadMTLSClient(certificatePath, privateKeyPath, rootCAPath string) (*http.Client, error) {
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return nil, errStartupTLS
	}
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, errStartupTLS
	}
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return nil, errStartupTLS
	}
	rootPEM, err := os.ReadFile(rootCAPath)
	if err != nil {
		return nil, errStartupTLS
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, errStartupTLS
	}
	client, err := agent.NewMTLSHTTPClient(certificate, roots)
	if err != nil {
		return nil, errStartupTLS
	}
	return client, nil
}

func loadIngests(path, ffmpegBinary string) ([]agent.IngestLoop, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errStartupSources
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxRTSPSourcesFile {
		return nil, errStartupSources
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxRTSPSourcesFile+1))
	decoder.DisallowUnknownFields()
	var payload sourceFile
	if err := decoder.Decode(&payload); err != nil || len(payload.Sources) == 0 || len(payload.Sources) > maxRTSPSources {
		return nil, errStartupSources
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errStartupSources
	}
	seen := make(map[string]struct{}, len(payload.Sources))
	ingests := make([]agent.IngestLoop, 0, len(payload.Sources))
	for _, source := range payload.Sources {
		id := strings.TrimSpace(source.ID)
		if _, duplicate := seen[id]; duplicate {
			return nil, errStartupSources
		}
		seen[id] = struct{}{}
		ingest, err := agent.NewRTSPIngest(agent.RTSPSource{ID: id, URL: source.URL, Transport: source.Transport}, agent.RTSPIngestConfig{
			Binary: ffmpegBinary,
			Decode: &agent.DecodeProfile{
				Codec:             agent.VideoCodec(source.Codec),
				Preference:        agent.DecodePreference(source.DecodePreference),
				AvailableDecoders: append([]string(nil), source.AvailableDecoders...),
			},
		}, nil)
		if err != nil {
			return nil, errStartupSources
		}
		ingests = append(ingests, ingest)
	}
	return ingests, nil
}

func (l *runtimeLogger) event(event agent.RuntimeEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.encoder.Encode(event)
}

func (l *runtimeLogger) startupFailure(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.encoder.Encode(map[string]string{"component": "runtime", "state": "startup_failed", "reason": reason})
}
