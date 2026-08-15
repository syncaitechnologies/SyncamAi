package agent

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestNormalizeHealthBoundsAndThermalClassification(t *testing.T) {
	for _, test := range []struct {
		temperature float64
		want        ThermalState
	}{
		{temperature: 79.9, want: ThermalNormal},
		{temperature: ThermalWarningCelsius, want: ThermalWarning},
		{temperature: ThermalCriticalCelsius, want: ThermalCritical},
	} {
		health, err := NormalizeHealth(HealthSample{CPUUtilizationPercent: 45.5, GPUUtilizationPercent: 67.25, TemperatureCelsius: test.temperature, InferenceLatencyMs: 12.5})
		if err != nil || health.ThermalState != test.want {
			t.Fatalf("temperature %.1f: health=%+v err=%v", test.temperature, health, err)
		}
	}
	for _, sample := range []HealthSample{
		{CPUUtilizationPercent: -1},
		{CPUUtilizationPercent: 101},
		{GPUUtilizationPercent: 101},
		{TemperatureCelsius: 151},
		{InferenceLatencyMs: -1},
		{CPUUtilizationPercent: math.NaN()},
	} {
		if _, err := NormalizeHealth(sample); !errors.Is(err, ErrInvalidHealthTelemetry) {
			t.Fatalf("sample %+v: expected validation error, got %v", sample, err)
		}
	}
}

func TestHealthMonitorReportsSamplesAndContinuesAfterProbeError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	monitor, err := NewHealthMonitor(HealthProbeFunc(func(context.Context) (HealthSample, error) {
		attempts++
		if attempts == 1 {
			return HealthSample{}, errors.New("probe unavailable")
		}
		return HealthSample{CPUUtilizationPercent: 20, GPUUtilizationPercent: 30, TemperatureCelsius: 81, InferenceLatencyMs: 15}, nil
	}), time.Millisecond)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	reports := 0
	err = monitor.Run(ctx, func(health HealthTelemetry, sampleErr error) {
		reports++
		if reports == 1 && sampleErr == nil {
			t.Error("first probe error was not reported")
		}
		if reports == 2 {
			if sampleErr != nil || health.ThermalState != ThermalWarning {
				t.Errorf("unexpected recovered health: %+v err=%v", health, sampleErr)
			}
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || attempts != 2 || reports != 2 {
		t.Fatalf("unexpected monitor result: attempts=%d reports=%d err=%v", attempts, reports, err)
	}
}

func TestHealthMonitorRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewHealthMonitor(nil, time.Second); !errors.Is(err, ErrInvalidHealthTelemetry) {
		t.Fatalf("expected nil probe rejection, got %v", err)
	}
	probe := HealthProbeFunc(func(context.Context) (HealthSample, error) { return HealthSample{}, nil })
	if _, err := NewHealthMonitor(probe, HealthInterval+time.Second); !errors.Is(err, ErrInvalidHealthTelemetry) {
		t.Fatalf("expected slow interval rejection, got %v", err)
	}
}
