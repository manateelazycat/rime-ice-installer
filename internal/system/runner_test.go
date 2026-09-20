package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateSudoFingerprintSuccess(t *testing.T) {
	skipWhenRoot(t)
	installFakeSudo(t, "#!/bin/sh\nexit 0\n")
	runner := NewRunner(false, nil)
	if err := runner.ValidateSudoFingerprint(context.Background()); err != nil {
		t.Fatalf("expected fingerprint validation success, got %v", err)
	}
}

func TestValidateSudoFingerprintClosesInputAtPasswordFallback(t *testing.T) {
	skipWhenRoot(t)
	installFakeSudo(t, "#!/bin/sh\nprintf %s \\\"$3\\\" >&2\nif IFS= read -r input; then exit 42; fi\nexit 1\n")
	runner := NewRunner(false, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := runner.ValidateSudoFingerprint(ctx); err == nil {
		t.Fatal("password fallback should not authenticate")
	}
	if ctx.Err() != nil {
		t.Fatal("sudo input was not closed when the password prompt appeared")
	}
}

func skipWhenRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root does not need sudo authentication")
	}
}

func installFakeSudo(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sudo")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}
