package fcitx

import (
	"strings"
	"testing"

	"rime-ice-installer/internal/system"
)

func TestReplaceManagedBlock(t *testing.T) {
	original := "export FOO=bar\n"
	block := strings.Join([]string{
		managedBlockStart,
		"export GTK_IM_MODULE=fcitx",
		managedBlockEnd,
		"",
	}, "\n")

	updated := system.ReplaceOrAppendBlock(original, managedBlockStart, managedBlockEnd, block)
	if !strings.Contains(updated, "GTK_IM_MODULE=fcitx") {
		t.Fatalf("expected managed block to be appended, got: %s", updated)
	}

	updatedAgain := system.ReplaceOrAppendBlock(updated, managedBlockStart, managedBlockEnd, block)
	if strings.Count(updatedAgain, managedBlockStart) != 1 {
		t.Fatalf("expected managed block to be replaced in place, got: %s", updatedAgain)
	}
}

func TestPacmanPackagesIncludeOpenCC(t *testing.T) {
	found := false
	for _, pkg := range pacmanPackages {
		if pkg == "opencc" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected pacman package list to include opencc")
	}
}

func TestMissingLibrariesFromLdd(t *testing.T) {
	output := `
	libopencc.so.1.2 => not found
	libfoo.so => /usr/lib/libfoo.so
	libbar.so => not found
	libopencc.so.1.2 => not found
`
	got := missingLibrariesFromLdd(output)
	if len(got) != 2 || got[0] != "libopencc.so.1.2" || got[1] != "libbar.so" {
		t.Fatalf("unexpected missing libs: %#v", got)
	}
}

func TestRemoveAccentColorField(t *testing.T) {
	input := `[Metadata]
Name=Default Dark

[Menu]
Color=#000000

[AccentColorField]
0=Input Panel Border
1=Menu Selected Item Background
`
	output := removeAccentColorField(input)
	if strings.Contains(output, "[AccentColorField]") {
		t.Fatalf("accent field section should be removed: %s", output)
	}
	if !strings.Contains(output, "[Menu]") {
		t.Fatalf("non-accent sections should be preserved: %s", output)
	}
}

func TestClassicUIRuntimeConfigUsesInstallerDark(t *testing.T) {
	payload := classicUIRuntimeConfig()
	for _, expected := range []string{
		"'Theme': <'installer-dark'>",
		"'DarkTheme': <'installer-dark'>",
		"'UseAccentColor': <'False'>",
	} {
		if !strings.Contains(payload, expected) {
			t.Fatalf("runtime payload missing %s: %s", expected, payload)
		}
	}
}

func TestShortcutRuntimeConfigsClearTriggerKeys(t *testing.T) {
	for name, payload := range map[string]string{
		"clipboard":   clipboardRuntimeConfig(),
		"quickphrase": quickPhraseRuntimeConfig(),
		"unicode":     unicodeRuntimeConfig(),
	} {
		if !strings.Contains(payload, "<@a{sv} {}>") {
			t.Fatalf("%s runtime payload should clear trigger keys: %s", name, payload)
		}
	}
}
