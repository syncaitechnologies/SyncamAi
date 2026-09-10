package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrInvalidRuntime = errors.New("edge runtime configuration is invalid")

type heartbeatLoop interface {
	Run(context.Context, time.Duration, func() Telemetry, func(HeartbeatResult, error)) error
}

type configurationLoop interface {
	AppliedRevision() int64
	Run(context.Context, time.Duration) error
	SyncIfDesired(context.Context, int64) error
}

type IngestLoop interface {
	Run(context.Context, func(RTSPStatus)) error
}

type spoolMetrics interface {
	Metrics() SpoolMetrics
}

// RuntimeEvent is deliberately credential-free. Callers may serialize it, but
// must not supplement it with source URLs, subprocess arguments, certificates,
// private keys, or raw transport errors.
type RuntimeEvent struct {
	Component string    `json:"component"`
	State     string    `json:"state"`
	SourceID  string    `json:"source_id,omitempty"`
	Attempt   int       `json:"attempt,omitempty"`
	Revision  int64     `json:"revision,omitempty"`
	At        time.Time `json:"at"`
}

type EdgeRuntimeConfig struct {
	FirmwareVersion       string
	HeartbeatInterval     time.Duration
	ConfigurationInterval time.Duration
}

// EdgeRuntime composes the already-reviewed heartbeat, configuration, spool,
// and RTSP lifecycle boundaries into one cancellable process. It does not
// retain frames or perform inference.
type EdgeRuntime struct {
	config        EdgeRuntimeConfig
	heartbeat     heartbeatLoop
	configuration configurationLoop
	spool         spoolMetrics
	ingests       []IngestLoop
	now           func() time.Time
}

func NewEdgeRuntime(config EdgeRuntimeConfig, heartbeat heartbeatLoop, configuration configurationLoop, spool spoolMetrics, ingests ...IngestLoop) (*EdgeRuntime, error) {
	config.FirmwareVersion = strings.TrimSpace(config.FirmwareVersion)
	if config.FirmwareVersion == "" || len(config.FirmwareVersion) > maxFirmwareVersionSize || config.HeartbeatInterval <= 0 || config.HeartbeatInterval > HeartbeatInterval || config.ConfigurationInterval <= 0 || config.ConfigurationInterval > ConfigPollInterval || heartbeat == nil || configuration == nil || spool == nil {
		return nil, ErrInvalidRuntime
	}
	for _, ingest := range ingests {
		if ingest == nil {
			return nil, ErrInvalidRuntime
		}
	}
	return &EdgeRuntime{
		config:        config,
		heartbeat:     heartbeat,
		configuration: configuration,
		spool:         spool,
		ingests:       append([]IngestLoop(nil), ingests...),
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *EdgeRuntime) Run(ctx context.Context, report func(RuntimeEvent)) error {
	if r == nil || report == nil {
		return ErrInvalidRuntime
	}
	startedAt := r.now()
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	report(r.event("runtime", "started"))

	var wait sync.WaitGroup
	errorsOut := make(chan error, len(r.ingests)+2)
	launch := func(run func() error) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsOut <- run()
		}()
	}

	launch(func() error {
		return r.configuration.Run(runContext, r.config.ConfigurationInterval)
	})
	for _, ingest := range r.ingests {
		ingest := ingest
		launch(func() error {
			return ingest.Run(runContext, func(status RTSPStatus) {
				report(RuntimeEvent{Component: "rtsp", State: string(status.State), SourceID: status.SourceID, Attempt: status.Attempt, At: r.now()})
			})
		})
	}
	launch(func() error {
		return r.heartbeat.Run(runContext, r.config.HeartbeatInterval, func() Telemetry {
			uptime := int64(r.now().Sub(startedAt).Seconds())
			if uptime < 0 {
				uptime = 0
			}
			return Telemetry{UptimeSeconds: uptime, StoreForwardDepth: r.spool.Metrics().Depth, FirmwareVersion: r.config.FirmwareVersion}
		}, func(result HeartbeatResult, err error) {
			if err != nil {
				report(r.event("heartbeat", "degraded"))
				return
			}
			report(r.event("heartbeat", "online"))
			if result.Device.DesiredConfigRevision <= r.configuration.AppliedRevision() {
				return
			}
			if err := r.configuration.SyncIfDesired(runContext, result.Device.DesiredConfigRevision); err != nil {
				report(RuntimeEvent{Component: "configuration", State: "degraded", Revision: result.Device.DesiredConfigRevision, At: r.now()})
				return
			}
			report(RuntimeEvent{Component: "configuration", State: "synchronized", Revision: r.configuration.AppliedRevision(), At: r.now()})
		})
	})

	var result error
	select {
	case <-ctx.Done():
		result = ctx.Err()
	case err := <-errorsOut:
		if err != nil && !errors.Is(err, context.Canceled) {
			result = err
		} else if ctx.Err() != nil {
			result = ctx.Err()
		} else {
			result = ErrInvalidRuntime
		}
	}
	cancel()
	wait.Wait()
	report(r.event("runtime", "stopped"))
	return result
}

func (r *EdgeRuntime) event(component, state string) RuntimeEvent {
	return RuntimeEvent{Component: component, State: state, At: r.now()}
}
