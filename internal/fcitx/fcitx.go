package fcitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/ini.v1"

	"rime-ice-installer/internal/config"
	"rime-ice-installer/internal/system"
)

var pacmanPackages = []string{
	"fcitx5",
	"fcitx5-gtk",
	"fcitx5-qt",
	"fcitx5-configtool",
	"fcitx5-rime",
	"librime",
	"opencc",
}

const (
	managedBlockStart = "# >>> rime-ice-installer ime begin >>>"
	managedBlockEnd   = "# <<< rime-ice-installer ime end <<<"
	customThemeName   = "installer-dark"
	systemThemeName   = "default-dark"
	keyboardUS        = "keyboard-us"
	defaultGroupName  = "Default"
)

func InstallPackages(ctx context.Context, runner *system.Runner, cfg config.InstallConfig, env config.DetectedEnv) error {
	args := append([]string{"-S", "--needed", "--noconfirm"}, pacmanPackages...)
	if err := runner.RunPrivileged(ctx, "pacman", args...); err != nil {
		return err
	}

	if !env.HasOctagramPlugin {
		if _, err := os.Stat(env.OctagramPluginPath); err == nil {
			env.HasOctagramPlugin = true
		}
	}
	if _, err := os.Stat(env.OctagramPluginPath); err != nil {
		return fmt.Errorf("未找到 octagram 插件: %s，当前 Arch 的 librime 应包含该插件", env.OctagramPluginPath)
	}
	return nil
}

func ValidateRimeRuntime(ctx context.Context, runner *system.Runner) error {
	output, err := runner.RunCapture(ctx, "ldd", "/usr/lib/fcitx5/librime.so")
	if err != nil {
		return err
	}

	missingLibs := missingLibrariesFromLdd(output)
	if len(missingLibs) == 0 {
		return nil
	}

	hint := "请先执行 sudo pacman -Syu opencc librime fcitx5-rime 再重试。"
	if slices.Contains(missingLibs, "libopencc.so.1.2") {
		hint = "检测到 opencc 与 librime 的 soname 不匹配，请先执行 sudo pacman -Syu opencc librime fcitx5-rime。"
	}
	return fmt.Errorf("Fcitx5 Rime 运行库缺失: %s。%s", strings.Join(missingLibs, ", "), hint)
}

func EnsureIMEEnvironment(env config.DetectedEnv) (string, error) {
	if env.EnvironmentFilePath == "" {
		return "", fmt.Errorf("无法确定 IM 环境变量配置文件路径")
	}

	existing, err := os.ReadFile(env.EnvironmentFilePath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("读取环境变量文件失败: %w", err)
	}

	block := strings.Join([]string{
		managedBlockStart,
		"export GTK_IM_MODULE=fcitx",
		"export QT_IM_MODULE=fcitx",
		"export XMODIFIERS=\"@im=fcitx\"",
		managedBlockEnd,
		"",
	}, "\n")
	updated := system.ReplaceOrAppendBlock(string(existing), managedBlockStart, managedBlockEnd, block)
	if err := system.WriteFileAtomic(env.EnvironmentFilePath, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return env.EnvironmentFilePath, nil
}

func Configure(home string, fontSize int) ([]string, error) {
	if err := ensureCustomTheme(home); err != nil {
		return nil, err
	}

	confDir := filepath.Join(home, ".config", "fcitx5", "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 Fcitx5 配置目录失败: %w", err)
	}

	paths := []string{
		filepath.Join(confDir, "classicui.conf"),
		filepath.Join(confDir, "clipboard.conf"),
		filepath.Join(confDir, "quickphrase.conf"),
		filepath.Join(confDir, "unicode.conf"),
		filepath.Join(home, ".config", "fcitx5", "config"),
		filepath.Join(home, ".config", "fcitx5", "profile"),
	}

	for _, path := range paths[4:] {
		if _, _, err := system.BackupFileWithSuffix(path, "_bak"); err != nil {
			return nil, err
		}
	}

	if err := writeDefaultSection(paths[0], map[string]string{
		"Vertical Candidate List": "False",
		"PerScreenDPI":            "False",
		"Font":                    fmt.Sprintf("%q", candidateFont(fontSize)),
		"Theme":                   customThemeName,
		"DarkTheme":               customThemeName,
		"UseDarkTheme":            "False",
		"UseAccentColor":          "False",
	}); err != nil {
		return nil, err
	}

	if err := writeDefaultSection(paths[1], map[string]string{
		"TriggerKey":                        "",
		"PastePrimaryKey":                   "",
		"Number of entries":                 "5",
		"IgnorePasswordFromPasswordManager": "False",
		"ShowPassword":                      "False",
		"ClearPasswordAfter":                "30",
	}); err != nil {
		return nil, err
	}

	if err := writeDefaultSection(paths[2], map[string]string{
		"TriggerKey":            "",
		"Choose Modifier":       "None",
		"Spell":                 "True",
		"FallbackSpellLanguage": "en",
	}); err != nil {
		return nil, err
	}

	if err := writeDefaultSection(paths[3], map[string]string{
		"TriggerKey":        "",
		"DirectUnicodeMode": "",
	}); err != nil {
		return nil, err
	}

	if err := ensureGlobalConfig(paths[4]); err != nil {
		return nil, err
	}

	if err := ensureProfile(paths[5]); err != nil {
		return nil, err
	}

	return paths, nil
}

func SyncRuntimeConfig(ctx context.Context, runner *system.Runner, fontSize int) error {
	if !fcitxRunning() {
		return nil
	}

	// ReloadConfig does not reliably reload the input method group from profile.
	// Apply it through the controller so the running instance can switch to Rime.
	if err := runner.Run(
		ctx, "gdbus", "call", "--session",
		"--dest", "org.fcitx.Fcitx5",
		"--object-path", "/controller",
		"--method", "org.fcitx.Fcitx.Controller1.SetInputMethodGroupInfo",
		defaultGroupName, "us", "[('keyboard-us', ''), ('rime', '')]",
	); err != nil {
		return fmt.Errorf("设置 Fcitx5 运行时输入法组失败: %w", err)
	}

	configs := []struct {
		path    string
		payload string
	}{
		{
			path:    "fcitx://config/global",
			payload: globalRuntimeConfig(),
		},
		{
			path:    "fcitx://config/addon/classicui",
			payload: classicUIRuntimeConfig(fontSize),
		},
		{
			path:    "fcitx://config/addon/clipboard",
			payload: clipboardRuntimeConfig(),
		},
		{
			path:    "fcitx://config/addon/quickphrase",
			payload: quickPhraseRuntimeConfig(),
		},
		{
			path:    "fcitx://config/addon/unicode",
			payload: unicodeRuntimeConfig(),
		},
	}

	for _, item := range configs {
		if err := runner.Run(
			ctx,
			"gdbus",
			"call",
			"--session",
			"--dest", "org.fcitx.Fcitx5",
			"--object-path", "/controller",
			"--method", "org.fcitx.Fcitx.Controller1.SetConfig",
			item.path,
			item.payload,
		); err != nil {
			return err
		}
	}

	if err := runner.Run(ctx, "dbus-send", "--session", "--dest=org.fcitx.Fcitx5", "/controller", "org.fcitx.Fcitx.Controller1.Save"); err != nil {
		return err
	}
	group, err := runner.RunCapture(
		ctx, "gdbus", "call", "--session",
		"--dest", "org.fcitx.Fcitx5",
		"--object-path", "/controller",
		"--method", "org.fcitx.Fcitx.Controller1.FullInputMethodGroupInfo",
		defaultGroupName,
	)
	if err != nil {
		return err
	}
	if !strings.Contains(group, "('keyboard-us',") || !strings.Contains(group, "('rime',") {
		return fmt.Errorf("Fcitx5 运行时输入法组未包含 keyboard-us 和 rime: %s", group)
	}

	output, err := runner.RunCapture(
		ctx,
		"gdbus",
		"call",
		"--session",
		"--dest", "org.fcitx.Fcitx5",
		"--object-path", "/controller",
		"--method", "org.fcitx.Fcitx.Controller1.GetConfig",
		"fcitx://config/global",
	)
	if err != nil {
		return err
	}
	// Only the first struct of the reply carries the live values; the trailing
	// part repeats the schema defaults, which contain the same shortcut names and
	// therefore must not be used for verification.
	for _, expected := range []string{
		"'TriggerKeys': <{'0': <'Control+space'>}>",
		"'EnumerateForwardKeys': <{'0': <'Shift_L'>, '1': <'Shift_R'>}>",
		"'ModifierOnlyKeyTimeout': <'-1'>",
	} {
		if !strings.Contains(output, expected) {
			return fmt.Errorf("Fcitx5 运行时快捷键配置未生效，缺少 %s", expected)
		}
	}
	return nil
}

func writeDefaultSection(path string, values map[string]string) error {
	cfg, err := ini.LooseLoad(path)
	if err != nil {
		return fmt.Errorf("加载配置失败 %s: %w", path, err)
	}
	section := cfg.Section("")
	for key, value := range values {
		section.Key(key).SetValue(value)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := saveFcitxIni(cfg, path); err != nil {
		return fmt.Errorf("保存配置失败 %s: %w", path, err)
	}
	return nil
}

// numericKeyPattern matches an INI entry whose key is a list index.
var numericKeyPattern = regexp.MustCompile(`^(\d+)\s*=\s*(.*)$`)

// saveFcitxIni writes cfg in the layout fcitx5 is able to read back.
//
// gopkg.in/ini.v1 pretty prints every entry as "key = value". fcitx5 parses the
// keys of list sections (for example "[Hotkey/TriggerKeys]" with key "0") as
// integers and silently drops entries whose key is padded, so "0 = Control+space"
// is loaded as an empty shortcut list and Ctrl+Space stops working after a
// restart. Numeric keys therefore have to be written as "0=Control+space".
func saveFcitxIni(cfg *ini.File, path string) error {
	var buf bytes.Buffer
	if _, err := cfg.WriteTo(&buf); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(compactNumericKeys(buf.String())), 0o644)
}

func compactNumericKeys(content string) string {
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		if match := numericKeyPattern.FindStringSubmatch(line); match != nil {
			lines[index] = match[1] + "=" + match[2]
		}
	}
	return strings.Join(lines, "\n")
}

// paddedNumericKey returns the first list entry with blanks around "=" so it
// can be reported instead of failing silently inside fcitx5.
func paddedNumericKey(content string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		match := numericKeyPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if match[1]+"="+match[2] != line {
			return line, true
		}
	}
	return "", false
}

func verifyShortcutConfig(content string) error {
	if line, padded := paddedNumericKey(content); padded {
		return fmt.Errorf("Fcitx5 无法解析带空格填充的列表项 %q", line)
	}
	for _, expected := range []string{
		"[Hotkey/TriggerKeys]\n0=Control+space",
		"[Hotkey/EnumerateForwardKeys]\n0=Shift_L\n1=Shift_R",
	} {
		if !strings.Contains(content, expected) {
			return fmt.Errorf("Fcitx5 快捷键配置缺少 %q", strings.ReplaceAll(expected, "\n", ", "))
		}
	}
	return nil
}

func ensureGlobalConfig(path string) error {
	cfg, err := ini.LooseLoad(path)
	if err != nil {
		return fmt.Errorf("加载 Fcitx5 全局配置失败: %w", err)
	}

	hotkey := cfg.Section("Hotkey")
	hotkey.Key("ModifierOnlyKeyTimeout").SetValue("-1")
	hotkey.Key("AltTriggerKeys").SetValue("")

	triggerKeys := cfg.Section("Hotkey/TriggerKeys")
	triggerKeys.Key("0").SetValue("Control+space")

	// This installer promises that either Shift key switches directly between
	// keyboard-us and Rime. Replace this list so stale entries cannot make the
	// behavior depend on a previous Fcitx5 configuration.
	cfg.DeleteSection("Hotkey/EnumerateForwardKeys")
	enumerateKeys := cfg.Section("Hotkey/EnumerateForwardKeys")
	enumerateKeys.Key("0").SetValue("Shift_L")
	enumerateKeys.Key("1").SetValue("Shift_R")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 Fcitx5 配置目录失败: %w", err)
	}
	if err := saveFcitxIni(cfg, path); err != nil {
		return fmt.Errorf("保存 Fcitx5 全局配置失败: %w", err)
	}

	// The ini library round trips both "0 = value" and "0=value", while fcitx5
	// only accepts the compact form. Verify the bytes on disk so a formatting
	// regression fails here instead of after the next login.
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 Fcitx5 全局配置失败: %w", err)
	}
	if err := verifyShortcutConfig(string(content)); err != nil {
		return err
	}
	return nil
}

func ensureProfile(path string) error {
	cfg, err := ini.LooseLoad(path)
	if err != nil {
		return fmt.Errorf("加载 profile 失败: %w", err)
	}

	// Normalize the default group to exactly two entries. This makes both
	// Ctrl+Space and the Shift enumeration shortcut deterministic while keeping
	// any additional input-method groups untouched.
	for _, section := range cfg.Sections() {
		if strings.HasPrefix(section.Name(), "Groups/0/Items/") {
			cfg.DeleteSection(section.Name())
		}
	}

	group := cfg.Section("Groups/0")
	group.Key("Name").SetValue(defaultGroupName)
	group.Key("Default Layout").SetValue("us")
	// The first item provides the inactive English keyboard. DefaultIM is the
	// input method activated by Ctrl+Space, so it must be Rime rather than the
	// keyboard item.
	group.Key("DefaultIM").SetValue("rime")

	keyboardSection := cfg.Section("Groups/0/Items/0")
	keyboardSection.Key("Name").SetValue(keyboardUS)
	keyboardSection.Key("Layout").SetValue("")

	rimeSection := cfg.Section("Groups/0/Items/1")
	rimeSection.Key("Name").SetValue("rime")
	rimeSection.Key("Layout").SetValue("")

	// Keep GroupOrder after the item sections, matching the profile Fcitx writes.
	// A group order section between Groups/0 and its items can leave only the
	// keyboard item in the live group when Fcitx starts.
	previousOrder := make([][2]string, 0)
	if cfg.HasSection("GroupOrder") {
		for _, key := range cfg.Section("GroupOrder").Keys() {
			previousOrder = append(previousOrder, [2]string{key.Name(), key.Value()})
		}
		cfg.DeleteSection("GroupOrder")
	}
	order := cfg.Section("GroupOrder")
	for _, entry := range previousOrder {
		order.Key(entry[0]).SetValue(entry[1])
	}
	order.Key("0").SetValue(defaultGroupName)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 profile 目录失败: %w", err)
	}
	// GroupOrder uses numeric keys as well, which fcitx5 rejects in padded form.
	if err := saveFcitxIni(cfg, path); err != nil {
		return fmt.Errorf("保存 profile 失败: %w", err)
	}
	return nil
}

func Reload(ctx context.Context, runner *system.Runner) error {
	if !fcitxRunning() {
		return nil
	}
	if err := runner.Run(ctx, "fcitx5-remote", "-r"); err != nil {
		return err
	}
	return runner.Run(ctx, "dbus-send", "--session", "--dest=org.fcitx.Fcitx5", "/controller", "org.fcitx.Fcitx.Controller1.ReloadConfig")
}

func Restart(ctx context.Context, runner *system.Runner) error {
	if !fcitxRunning() {
		return nil
	}
	return runner.Run(ctx, "dbus-send", "--session", "--dest=org.fcitx.Fcitx5", "/controller", "org.fcitx.Fcitx.Controller1.Restart")
}

func ensureCustomTheme(home string) error {
	sourceDir := filepath.Join("/usr/share/fcitx5/themes", systemThemeName)
	targetDir := filepath.Join(home, ".local", "share", "fcitx5", "themes", customThemeName)

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("清理旧主题失败: %w", err)
	}
	if err := system.CopyDir(sourceDir, targetDir); err != nil {
		return fmt.Errorf("复制基础主题失败: %w", err)
	}
	if err := writeCustomThemeAssets(targetDir); err != nil {
		return fmt.Errorf("写入自定义主题资源失败: %w", err)
	}
	return nil
}

func candidateFont(fontSize int) string {
	return fmt.Sprintf("Noto Sans Mono %d", fontSize)
}

func globalRuntimeConfig() string {
	return `<{'Hotkey': <{'TriggerKeys': <{'0': <'Control+space'>}>, 'AltTriggerKeys': <@a{sv} {}>, 'EnumerateForwardKeys': <{'0': <'Shift_L'>, '1': <'Shift_R'>}>, 'ModifierOnlyKeyTimeout': <'-1'>}>}>`
}

func classicUIRuntimeConfig(fontSize int) string {
	return fmt.Sprintf(`<{'Vertical Candidate List': <'False'>, 'PerScreenDPI': <'False'>, 'Font': <'%s'>, 'Theme': <'installer-dark'>, 'DarkTheme': <'installer-dark'>, 'UseDarkTheme': <'False'>, 'UseAccentColor': <'False'>}>`, candidateFont(fontSize))
}

func clipboardRuntimeConfig() string {
	return `<{'TriggerKey': <@a{sv} {}>, 'PastePrimaryKey': <@a{sv} {}>, 'Number of entries': <'5'>, 'IgnorePasswordFromPasswordManager': <'False'>, 'ShowPassword': <'False'>, 'ClearPasswordAfter': <'30'>}>`
}

func quickPhraseRuntimeConfig() string {
	return `<{'TriggerKey': <@a{sv} {}>, 'Choose Modifier': <'None'>, 'Spell': <'True'>, 'FallbackSpellLanguage': <'en'>}>`
}

func unicodeRuntimeConfig() string {
	return `<{'TriggerKey': <@a{sv} {}>, 'DirectUnicodeMode': <@a{sv} {}>}>`
}

func missingLibrariesFromLdd(output string) []string {
	seen := map[string]bool{}
	result := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "=> not found") {
			continue
		}
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

func fcitxRunning() bool {
	cmd := exec.Command("fcitx5-remote", "--check")
	return cmd.Run() == nil
}
