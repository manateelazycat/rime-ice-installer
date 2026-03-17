package wanxiang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeCustomConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rime_ice.custom.yaml")
	initial := "patch:\n  menu/page_size: 9\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial yaml: %v", err)
	}

	if err := MergeCustomConfig(path); err != nil {
		t.Fatalf("merge custom config: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged yaml: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, "menu/page_size: 9") {
		t.Fatalf("expected existing patch to be preserved, got: %s", text)
	}
	if !strings.Contains(text, "grammar/language: wanxiang-lts-zh-hans") {
		t.Fatalf("expected grammar language patch to be written, got: %s", text)
	}
	if !strings.Contains(text, "translator/max_homophones: 5") {
		t.Fatalf("expected translator patch to be written, got: %s", text)
	}
}
