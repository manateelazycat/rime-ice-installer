package rime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchDefaultYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.yaml")
	content := strings.Join([]string{
		"menu:",
		"  page_size: 5",
		"key_binder:",
		"  bindings:",
		"    # - { when: has_menu, accept: comma, send: Page_Up }",
		"    # - { when: has_menu, accept: period, send: Page_Down }",
		"",
	}, "\n")

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	if err := PatchDefaultYAML(path); err != nil {
		t.Fatalf("patch default yaml: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated yaml: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, "page_size: 9") {
		t.Fatalf("expected page_size to be updated, got: %s", text)
	}
	if strings.Contains(text, "# - { when: has_menu, accept: comma, send: Page_Up }") {
		t.Fatalf("expected comma page up binding to be uncommented, got: %s", text)
	}
	if strings.Contains(text, "# - { when: has_menu, accept: period, send: Page_Down }") {
		t.Fatalf("expected period page down binding to be uncommented, got: %s", text)
	}
}

func TestEnsureActiveSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.yaml")
	initial := "var:\n  last_build_time: 1\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial user.yaml: %v", err)
	}

	if err := EnsureActiveSchema(path); err != nil {
		t.Fatalf("ensure active schema: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read user.yaml: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, "previously_selected_schema: rime_ice") {
		t.Fatalf("expected active schema to be rime_ice, got: %s", text)
	}
	if !strings.Contains(text, "last_build_time: 1") {
		t.Fatalf("expected existing fields to be preserved, got: %s", text)
	}
}
