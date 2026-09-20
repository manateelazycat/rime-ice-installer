package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintDBusOutputParsing(t *testing.T) {
	if got := defaultFingerprintDevice([]byte("(objectpath '/net/reactivated/Fprint/Device/0',)")); got != "/net/reactivated/Fprint/Device/0" {
		t.Fatalf("unexpected device path: %q", got)
	}
	if !hasEnrolledFingerprint([]byte("(['right-index-finger'],)")) {
		t.Fatal("expected enrolled fingerprint to be detected")
	}
	if hasEnrolledFingerprint([]byte("(@as [],)")) {
		t.Fatal("empty fingerprint list must not be detected as enrolled")
	}
}

func TestSudoPAMUsesFingerprintThroughInclude(t *testing.T) {
	dir := t.TempDir()
	sudoPath := filepath.Join(dir, "sudo")
	systemAuthPath := filepath.Join(dir, "system-auth")
	if err := os.WriteFile(sudoPath, []byte("#%PAM-1.0\nauth include system-auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(systemAuthPath, []byte("auth sufficient pam_fprintd.so\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !sudoPAMUsesFingerprint(sudoPath, map[string]bool{}) {
		t.Fatal("expected fingerprint module in included PAM service to be detected")
	}
}

func TestSudoPAMIgnoresCommentedFingerprintModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sudo")
	if err := os.WriteFile(path, []byte("# auth sufficient pam_fprintd.so\nauth include system-auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sudoPAMUsesFingerprint(path, map[string]bool{}) {
		t.Fatal("commented fingerprint module must not be detected")
	}
}
