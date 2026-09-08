package main

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Malformed synthetic URLs fail during parsing, before any external request.
func TestStartupLogsOmitSensitiveConfiguration(t *testing.T) {
	if os.Getenv("SYNCAM_TEST_LOG_HELPER") == "1" {
		log.SetFlags(0)
		main()
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartupLogsOmitSensitiveConfiguration$")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "SYNCAM_") {
			child.Env = append(child.Env, value)
		}
	}
	child.Env = append(child.Env,
		"SYNCAM_TEST_LOG_HELPER=1",
		"SYNCAM_DATABASE_URL=postgres://synthetic-user:synthetic-sensitive-marker@%invalid/db",
		"SYNCAM_WORKER_TENANT_ID=11111111-1111-4111-8111-111111111111",
		"SYNCAM_OIDC_ISSUER=https://synthetic-sensitive-marker@%invalid",
		"SYNCAM_OIDC_AUDIENCE=synthetic-audience",
		"SYNCAM_DEVICE_CLAIM_KEY="+base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32))),
		"SYNCAM_SUPABASE_URL=https://example.supabase.co",
		"SYNCAM_SUPABASE_SECRET_KEY=synthetic-sensitive-marker",
	)
	output, err := child.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("startup log test timed out")
	}
	if err == nil {
		t.Fatal("invalid configuration should fail startup")
	}
	if strings.TrimSpace(string(output)) != "configure OIDC verifier failed" {
		t.Fatalf("startup must emit only the safe failure category; got %q", output)
	}
}
