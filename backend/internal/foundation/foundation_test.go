package foundation

import "testing"

func TestName(t *testing.T) {
	if Name != "syncam-ai" {
		t.Fatalf("unexpected product name %q", Name)
	}
}
