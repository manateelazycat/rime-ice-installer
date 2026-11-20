package fcitx

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/ini.v1"

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

func TestGlobalRuntimeConfigContainsSwitchKeys(t *testing.T) {
	payload := globalRuntimeConfig()
	for _, expected := range []string{
		"'Control+space'",
		"'AltTriggerKeys': <@a{sv} {}>",
		"'Shift_L'",
		"'Shift_R'",
		"'ModifierOnlyKeyTimeout': <'-1'>",
		"'ActiveByDefault': <'True'>",
	} {
		if !strings.Contains(payload, expected) {
			t.Fatalf("global runtime payload missing %s: %s", expected, payload)
		}
	}
}

func TestActivateRimeUsesNewInstanceAndChecksSchema(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "fcitx5-remote"), []byte("#!/bin/sh\n[ \"$1\" = --check ]\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	const gdbus = `#!/bin/sh
case "$*" in
  *GetNameOwner*)
    if [ -f "$FCITX_TEST_RESTARTED" ]; then printf "(':1.2',)\n"; else printf "(':1.1',)\n"; fi
    ;;
  *"--dest :1.1 "*Controller1.Restart) : > "$FCITX_TEST_RESTARTED" ;;
  *"--dest :1.2 "*Controller1.SwitchInputMethodGroup\ Default) printf '()\n' ;;
  *"--dest :1.2 "*Controller1.SetCurrentIM\ rime) printf '()\n' ;;
  *"--dest :1.2 "*Controller1.CurrentInputMethod) printf "('rime',)\n" ;;
  *"--dest :1.2 "*Rime1.ListAllSchemas) printf "(['double_pinyin', 'rime_ice'],)\n" ;;
  *"--dest :1.2 "*Rime1.SetSchema\ rime_ice) printf '()\n' ;;
  *"--dest :1.2 "*Rime1.SetAsciiMode\ false) printf '()\n' ;;
  *"--dest :1.2 "*Rime1.GetCurrentSchema) printf "('%s',)\n" "$FCITX_TEST_SCHEMA" ;;
  *"--dest :1.2 "*Rime1.IsAsciiMode) printf '(false,)\n' ;;
  *"--dest :1.2 "*Controller1.Save) printf '()\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gdbus"), []byte(gdbus), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, schema := range []string{"rime_ice", "double_pinyin"} {
		t.Run(schema, func(t *testing.T) {
			t.Setenv("FCITX_TEST_RESTARTED", filepath.Join(t.TempDir(), "restarted"))
			t.Setenv("FCITX_TEST_SCHEMA", schema)
			activated, err := ActivateRime(context.Background(), system.NewRunner(false, nil), "rime_ice")
			if schema == "rime_ice" {
				if err != nil || !activated {
					t.Fatalf("expected verified activation, got %v, %v", activated, err)
				}
			} else if activated || err == nil || !strings.Contains(err.Error(), "double_pinyin") {
				t.Fatalf("expected old schema to fail activation, got %v, %v", activated, err)
			}
		})
	}
}

func TestClassicUIRuntimeConfigUsesInstallerDark(t *testing.T) {
	payload := classicUIRuntimeConfig(21)
	for _, expected := range []string{
		"'Font': <'Noto Sans Mono 21'>",
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

func TestEnsureProfileCreatesKeyboardUSAndRime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile")

	if err := ensureProfile(path); err != nil {
		t.Fatalf("ensureProfile failed: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	group := cfg.Section("Groups/0")
	if got := group.Key("Name").String(); got != defaultGroupName {
		t.Fatalf("expected group name %s, got %q", defaultGroupName, got)
	}
	if got := group.Key("Default Layout").String(); got != "us" {
		t.Fatalf("expected Default Layout us, got %q", got)
	}
	if got := group.Key("DefaultIM").String(); got != "rime" {
		t.Fatalf("expected DefaultIM rime, got %q", got)
	}
	if got := cfg.Section("Groups/0/Items/0").Key("Name").String(); got != keyboardUS {
		t.Fatalf("expected first item %s, got %q", keyboardUS, got)
	}
	if got := cfg.Section("Groups/0/Items/1").Key("Name").String(); got != "rime" {
		t.Fatalf("expected second item rime, got %q", got)
	}
	if got := cfg.Section("GroupOrder").Key("0").String(); got != defaultGroupName {
		t.Fatalf("expected group order %s, got %q", defaultGroupName, got)
	}
	profile, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if strings.Index(string(profile), "[GroupOrder]") < strings.Index(string(profile), "[Groups/0/Items/1]") {
		t.Fatalf("group order must follow the input method items: %s", profile)
	}
	if !strings.Contains(string(profile), "DefaultIM=rime\n") {
		t.Fatalf("Fcitx requires an unpadded key in the written profile:\n%s", profile)
	}
}

func TestEnsureProfileNormalizesExistingDefaultGroup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile")
	initial := strings.Join([]string{
		"[Groups/0]",
		"Name=默认",
		"DefaultIM=rime",
		"",
		"[Groups/0/Items/0]",
		"Name=keyboard-cn",
		"Layout=",
		"",
		"[Groups/0/Items/1]",
		"Name=rime",
		"Layout=",
		"",
		"[Groups/0/Items/2]",
		"Name=keyboard-fr",
		"Layout=",
		"",
		"[Groups/1]",
		"Name=Other",
		"",
		"[Groups/1/Items/0]",
		"Name=keyboard-de",
		"Layout=",
		"",
		"[GroupOrder]",
		"0=Other",
		"1=Default",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	if err := ensureProfile(path); err != nil {
		t.Fatalf("ensureProfile failed: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}

	group := cfg.Section("Groups/0")
	if got := group.Key("Name").String(); got != defaultGroupName {
		t.Fatalf("expected group name %s, got %q", defaultGroupName, got)
	}
	if got := group.Key("Default Layout").String(); got != "us" {
		t.Fatalf("expected Default Layout us, got %q", got)
	}
	if got := group.Key("DefaultIM").String(); got != "rime" {
		t.Fatalf("expected DefaultIM rime, got %q", got)
	}
	if got := cfg.Section("Groups/0/Items/0").Key("Name").String(); got != keyboardUS {
		t.Fatalf("expected first item %s, got %q", keyboardUS, got)
	}
	if got := cfg.Section("Groups/0/Items/1").Key("Name").String(); got != "rime" {
		t.Fatalf("expected second item rime, got %q", got)
	}
	if cfg.HasSection("Groups/0/Items/2") {
		t.Fatalf("expected default group to contain exactly two input methods")
	}
	if got := cfg.Section("GroupOrder").Key("0").String(); got != defaultGroupName {
		t.Fatalf("expected group order %s, got %q", defaultGroupName, got)
	}
	if got := cfg.Section("Groups/1/Items/0").Key("Name").String(); got != "keyboard-de" {
		t.Fatalf("expected other groups to be preserved, got %q", got)
	}
	if got := cfg.Section("GroupOrder").Key("1").String(); got != "Other" {
		t.Fatalf("expected other group order to be preserved, got %q", got)
	}

	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read normalized profile: %v", err)
	}
	if err := ensureProfile(path); err != nil {
		t.Fatalf("second ensureProfile failed: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile after second run: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("expected profile update to be idempotent\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestEnsureGlobalConfigSetsSwitchKeysAndPreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	initial := strings.Join([]string{
		"[Hotkey]",
		"AltTriggerKeys=Shift_L",
		"ModifierOnlyKeyTimeout=250",
		"",
		"[Hotkey/EnumerateForwardKeys]",
		"0=Control+Shift_L",
		"2=Alt+space",
		"",
		"[Behavior]",
		"ActiveByDefault=False",
		"ShareInputState=Program",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := ensureGlobalConfig(path); err != nil {
		t.Fatalf("ensureGlobalConfig failed: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Section("Hotkey").Key("ModifierOnlyKeyTimeout").String(); got != "-1" {
		t.Fatalf("expected no modifier timeout, got %q", got)
	}
	if got := cfg.Section("Hotkey").Key("AltTriggerKeys").String(); got != "" {
		t.Fatalf("expected empty AltTriggerKeys, got %q", got)
	}
	if got := cfg.Section("Hotkey/TriggerKeys").Key("0").String(); got != "Control+space" {
		t.Fatalf("expected Ctrl+Space trigger, got %q", got)
	}
	enumerate := cfg.Section("Hotkey/EnumerateForwardKeys")
	if got := enumerate.Key("0").String(); got != "Shift_L" {
		t.Fatalf("expected left Shift, got %q", got)
	}
	if got := enumerate.Key("1").String(); got != "Shift_R" {
		t.Fatalf("expected right Shift, got %q", got)
	}
	if enumerate.HasKey("2") {
		t.Fatalf("expected stale enumerate shortcuts to be removed")
	}
	if got := cfg.Section("Behavior").Key("ActiveByDefault").String(); got != "True" {
		t.Fatalf("expected Fcitx5 to activate Rime by default, got %q", got)
	}
	if got := cfg.Section("Behavior").Key("ShareInputState").String(); got != "Program" {
		t.Fatalf("expected unrelated setting to be preserved, got %q", got)
	}
}
