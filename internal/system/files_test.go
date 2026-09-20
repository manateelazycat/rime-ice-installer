package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupFileWithSuffixPreservesFirstBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	backupPath, created, err := BackupFileWithSuffix(path, "_bak")
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if !created {
		t.Fatalf("expected first backup to be created")
	}
	content, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("unexpected backup content: %q", content)
	}

	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatalf("update source: %v", err)
	}
	_, created, err = BackupFileWithSuffix(path, "_bak")
	if err != nil {
		t.Fatalf("reuse backup: %v", err)
	}
	if created {
		t.Fatalf("expected existing backup to be preserved")
	}
	content, err = os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read preserved backup: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("expected first backup to be preserved, got %q", content)
	}
}
