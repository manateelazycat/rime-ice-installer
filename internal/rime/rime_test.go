package rime

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeDefaultCustomConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.custom.yaml")
	initial := "patch:\n  switcher/hotkeys:\n    - F4\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial custom yaml: %v", err)
	}

	if err := MergeDefaultCustomConfig(path); err != nil {
		t.Fatalf("merge default custom yaml: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged yaml: %v", err)
	}

	root := map[string]any{}
	if err := yaml.Unmarshal(updated, &root); err != nil {
		t.Fatalf("unmarshal merged yaml: %v", err)
	}

	patch, ok := root["patch"].(map[string]any)
	if !ok || patch == nil {
		t.Fatalf("expected patch section, got: %#v", root["patch"])
	}

	if got := patch["menu/page_size"]; got != 9 {
		t.Fatalf("expected menu/page_size=9, got %#v", got)
	}
	if got := patch["ascii_composer/good_old_caps_lock"]; got != true {
		t.Fatalf("expected good_old_caps_lock=true, got %#v", got)
	}
	if got := patch["ascii_composer/switch_key/Shift_L"]; got != "inline_ascii" {
		t.Fatalf("expected Shift_L=inline_ascii, got %#v", got)
	}
	if got := patch["ascii_composer/switch_key/Shift_R"]; got != "noop" {
		t.Fatalf("expected Shift_R=noop, got %#v", got)
	}
	if got := patch["ascii_composer/switch_key/Control_L"]; got != "noop" {
		t.Fatalf("expected Control_L=noop, got %#v", got)
	}
	if got := patch["ascii_composer/switch_key/Control_R"]; got != "noop" {
		t.Fatalf("expected Control_R=noop, got %#v", got)
	}
	if _, ok := patch["switcher/hotkeys"]; !ok {
		t.Fatalf("expected existing patch entries to be preserved, got %#v", patch)
	}

	bindings, ok := patch["key_binder/bindings/+"].([]any)
	if !ok || len(bindings) != 2 {
		t.Fatalf("expected two appended bindings, got %#v", patch["key_binder/bindings/+"])
	}

	first, ok := bindings[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first binding map, got %#v", bindings[0])
	}
	if first["when"] != "has_menu" || first["accept"] != "comma" || first["send"] != "Page_Up" {
		t.Fatalf("unexpected first binding: %#v", first)
	}

	second, ok := bindings[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second binding map, got %#v", bindings[1])
	}
	if second["when"] != "has_menu" || second["accept"] != "period" || second["send"] != "Page_Down" {
		t.Fatalf("unexpected second binding: %#v", second)
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

	root := map[string]any{}
	if err := yaml.Unmarshal(updated, &root); err != nil {
		t.Fatalf("unmarshal user.yaml: %v", err)
	}
	varSection, ok := root["var"].(map[string]any)
	if !ok || varSection == nil {
		t.Fatalf("expected var section, got %#v", root["var"])
	}
	if got := varSection["previously_selected_schema"]; got != "rime_ice" {
		t.Fatalf("expected active schema to be rime_ice, got: %#v", got)
	}
	if got := varSection["last_build_time"]; got != 1 {
		t.Fatalf("expected existing fields to be preserved, got: %#v", got)
	}
}
