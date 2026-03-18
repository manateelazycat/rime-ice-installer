package fcitx

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
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

func TestCustomThemeConfigHasSciFiPalette(t *testing.T) {
	if strings.Contains(customThemeConf, "[AccentColorField]") {
		t.Fatalf("custom theme should not expose AccentColorField: %s", customThemeConf)
	}
	for _, unexpected := range []string{
		"[InputPanel/PrevPage]",
		"[InputPanel/NextPage]",
		"[Menu/SubMenu]",
		"PageButtonAlignment=",
	} {
		if strings.Contains(customThemeConf, unexpected) {
			t.Fatalf("custom theme should not contain %s: %s", unexpected, customThemeConf)
		}
	}

	for _, expected := range []string{
		"Name=Installer Dark Sci-Fi",
		"NormalColor=#d7dde5",
		"HighlightBackgroundColor=#27303a",
		"BorderColor=#090c11",
		"BorderWidth=0",
		"Image=radio.png",
	} {
		if !strings.Contains(customThemeConf, expected) {
			t.Fatalf("custom theme missing %s: %s", expected, customThemeConf)
		}
	}
}

func TestWriteCustomThemeAssets(t *testing.T) {
	dir := t.TempDir()
	if err := writeCustomThemeAssets(dir); err != nil {
		t.Fatalf("writeCustomThemeAssets failed: %v", err)
	}

	themePath := filepath.Join(dir, "theme.conf")
	content, err := os.ReadFile(themePath)
	if err != nil {
		t.Fatalf("read theme.conf: %v", err)
	}
	if string(content) != customThemeConf {
		t.Fatalf("unexpected theme.conf content: %s", string(content))
	}

	expectedSizes := map[string][2]int{
		"prev.png":  {16, 24},
		"next.png":  {16, 24},
		"arrow.png": {6, 12},
		"radio.png": {24, 24},
	}
	for name, size := range expectedSizes {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s should not be empty", name)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if cfg.Width != size[0] || cfg.Height != size[1] {
			t.Fatalf("%s size mismatch: got %dx%d want %dx%d", name, cfg.Width, cfg.Height, size[0], size[1])
		}
	}
}
