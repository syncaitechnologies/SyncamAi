package privacymasks

import (
	"context"
	"errors"
	"testing"
)

func TestValidateReleaseReportAllowsOnlySafeFixedStates(t *testing.T) {
	accepted := ReportReleaseCommand{DeviceID: releaseDeviceID, ReleaseID: "77777777-7777-4777-8777-777777777777", Version: 1, State: "accepted"}
	if err := validateReleaseReport(accepted); err != nil { t.Fatal(err) }
	failed := accepted; failed.State, failed.ErrorCode = "failed", "apply_failed"
	if err := validateReleaseReport(failed); err != nil { t.Fatal(err) }
	for _, invalid := range []ReportReleaseCommand{{}, func() ReportReleaseCommand { value := accepted; value.State = "failed"; return value }(), func() ReportReleaseCommand { value := accepted; value.State, value.ErrorCode = "failed", "encoder_details"; return value }()} { if !errors.Is(validateReleaseReport(invalid), ErrInvalidReleaseStatus) { t.Fatal("invalid release status must fail closed") } }
}

type memoryReleaseTransport struct { manifest *DeviceReleaseManifest; status DeviceReleaseStatus; err error; report ReportReleaseCommand }
func (m *memoryReleaseTransport) Pull(context.Context, string, int64) (PullReleaseResult, error) { return PullReleaseResult{Manifest: m.manifest}, m.err }
func (m *memoryReleaseTransport) Report(_ context.Context, command ReportReleaseCommand) (DeviceReleaseStatus, error) { m.report = command; return m.status, m.err }
