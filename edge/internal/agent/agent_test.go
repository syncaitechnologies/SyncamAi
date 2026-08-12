package agent

import "testing"

func TestRuntime(t *testing.T) {
	if Runtime != "go" {
		t.Fatalf("edge runtime must be Go, got %q", Runtime)
	}
}
