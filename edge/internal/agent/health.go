package agent

import (
	"context"
	"errors"
	"math"
	"time"
)

const (
	HealthInterval            = 30 * time.Second
	ThermalWarningCelsius     = 80.0
	ThermalCriticalCelsius    = 90.0
	maxInferenceLatencyMillis = 600000.0
	minimumTemperatureCelsius = -40.0
	maximumTemperatureCelsius = 150.0
)

var ErrInvalidHealthTelemetry = errors.New("edge health telemetry is invalid")

type ThermalState string

const (
	ThermalNormal   ThermalState = "normal"
	ThermalWarning  ThermalState = "warning"
	ThermalCritical ThermalState = "critical"
)

// HealthSample is collected inside the trusted device boundary. Platform
// adapters may obtain it from Jetson, NVIDIA, or OS-specific probes; the
// monitor validates and classifies every sample before it can be transported.
type HealthSample struct {
	CPUUtilizationPercent float64
	GPUUtilizationPercent float64
	TemperatureCelsius    float64
	InferenceLatencyMs    float64
}

type HealthTelemetry struct {
	CPUUtilizationPercent float64      `json:"cpu_utilization_percent"`
	GPUUtilizationPercent float64      `json:"gpu_utilization_percent"`
	TemperatureCelsius    float64      `json:"temperature_celsius"`
	InferenceLatencyMs    float64      `json:"inference_latency_ms"`
	ThermalState          ThermalState `json:"thermal_state"`
}

type HealthProbe interface {
	Sample(context.Context) (HealthSample, error)
}

type HealthProbeFunc func(context.Context) (HealthSample, error)

func (f HealthProbeFunc) Sample(ctx context.Context) (HealthSample, error) { return f(ctx) }

type HealthMonitor struct {
	probe    HealthProbe
	interval time.Duration
}

func NewHealthMonitor(probe HealthProbe, interval time.Duration) (*HealthMonitor, error) {
	if probe == nil || interval <= 0 || interval > HealthInterval {
		return nil, ErrInvalidHealthTelemetry
	}
	return &HealthMonitor{probe: probe, interval: interval}, nil
}

// Run samples immediately and then no slower than the canonical 30-second
// interval. Probe errors are reported without stopping later samples.
func (m *HealthMonitor) Run(ctx context.Context, report func(HealthTelemetry, error)) error {
	if m == nil || m.probe == nil || report == nil || m.interval <= 0 || m.interval > HealthInterval {
		return ErrInvalidHealthTelemetry
	}
	sample := func() {
		raw, err := m.probe.Sample(ctx)
		if err != nil {
			report(HealthTelemetry{}, err)
			return
		}
		health, err := NormalizeHealth(raw)
		report(health, err)
	}
	sample()
	if err := ctx.Err(); err != nil {
		return err
	}
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			sample()
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
}

func NormalizeHealth(sample HealthSample) (HealthTelemetry, error) {
	values := []float64{sample.CPUUtilizationPercent, sample.GPUUtilizationPercent, sample.TemperatureCelsius, sample.InferenceLatencyMs}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return HealthTelemetry{}, ErrInvalidHealthTelemetry
		}
	}
	if sample.CPUUtilizationPercent < 0 || sample.CPUUtilizationPercent > 100 || sample.GPUUtilizationPercent < 0 || sample.GPUUtilizationPercent > 100 || sample.TemperatureCelsius < minimumTemperatureCelsius || sample.TemperatureCelsius > maximumTemperatureCelsius || sample.InferenceLatencyMs < 0 || sample.InferenceLatencyMs > maxInferenceLatencyMillis {
		return HealthTelemetry{}, ErrInvalidHealthTelemetry
	}
	return HealthTelemetry{
		CPUUtilizationPercent: sample.CPUUtilizationPercent,
		GPUUtilizationPercent: sample.GPUUtilizationPercent,
		TemperatureCelsius:    sample.TemperatureCelsius,
		InferenceLatencyMs:    sample.InferenceLatencyMs,
		ThermalState:          classifyThermalState(sample.TemperatureCelsius),
	}, nil
}

func validHealthTelemetry(health HealthTelemetry) bool {
	normalized, err := NormalizeHealth(HealthSample{
		CPUUtilizationPercent: health.CPUUtilizationPercent,
		GPUUtilizationPercent: health.GPUUtilizationPercent,
		TemperatureCelsius:    health.TemperatureCelsius,
		InferenceLatencyMs:    health.InferenceLatencyMs,
	})
	return err == nil && normalized.ThermalState == health.ThermalState
}

func classifyThermalState(temperature float64) ThermalState {
	if temperature >= ThermalCriticalCelsius {
		return ThermalCritical
	}
	if temperature >= ThermalWarningCelsius {
		return ThermalWarning
	}
	return ThermalNormal
}
