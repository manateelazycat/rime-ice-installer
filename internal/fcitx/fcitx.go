package fcitx

import (
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

func Configure(home string) ([]string, error) {
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
		filepath.Join(home, ".config", "fcitx5", "profile"),
	}

	if err := writeDefaultSection(paths[0], map[string]string{
		"Vertical Candidate List": "False",
		"PerScreenDPI":            "False",
		"Font":                    `"Noto Sans Mono 13"`,
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

	if err := ensureProfile(paths[4]); err != nil {
		return nil, err
	}

	return paths, nil
}

func SyncRuntimeConfig(ctx context.Context, runner *system.Runner) error {
	if !fcitxRunning() {
		return nil
	}

	configs := []struct {
		path    string
		payload string
	}{
		{
			path:    "fcitx://config/addon/classicui",
			payload: classicUIRuntimeConfig(),
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

	return runner.Run(ctx, "dbus-send", "--session", "--dest=org.fcitx.Fcitx5", "/controller", "org.fcitx.Fcitx.Controller1.Save")
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
	if err := cfg.SaveTo(path); err != nil {
		return fmt.Errorf("保存配置失败 %s: %w", path, err)
	}
	return nil
}

func ensureProfile(path string) error {
	cfg, err := ini.LooseLoad(path)
	if err != nil {
		return fmt.Errorf("加载 profile 失败: %w", err)
	}

	group := cfg.Section("Groups/0")
	group.Key("Name").SetValue("默认")
	if group.Key("Default Layout").String() == "" {
		group.Key("Default Layout").SetValue("cn")
	}
	group.Key("DefaultIM").SetValue("rime")

	itemPattern := regexp.MustCompile(`^Groups/0/Items/(\d+)$`)
	maxIndex := -1
	hasAnyItem := false
	rimeFound := false

	for _, section := range cfg.Sections() {
		matches := itemPattern.FindStringSubmatch(section.Name())
		if matches == nil {
			continue
		}
		hasAnyItem = true
		index := 0
		fmt.Sscanf(matches[1], "%d", &index)
		if index > maxIndex {
			maxIndex = index
		}
		if section.Key("Name").String() == "rime" {
			rimeFound = true
			if section.Key("Layout").String() == "" {
				section.Key("Layout").SetValue("")
			}
		}
	}

	if !hasAnyItem {
		keyboardSection := cfg.Section("Groups/0/Items/0")
		keyboardSection.Key("Name").SetValue("keyboard-cn")
		keyboardSection.Key("Layout").SetValue("")
		maxIndex = 0
	}

	if !rimeFound {
		rimeSection := cfg.Section(fmt.Sprintf("Groups/0/Items/%d", maxIndex+1))
		rimeSection.Key("Name").SetValue("rime")
		rimeSection.Key("Layout").SetValue("")
	}

	order := cfg.Section("GroupOrder")
	if order.Key("0").String() == "" {
		order.Key("0").SetValue("默认")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 profile 目录失败: %w", err)
	}
	if err := cfg.SaveTo(path); err != nil {
		return fmt.Errorf("保存 profile 失败: %w", err)
	}
	return nil
}

func ReloadAndActivate(ctx context.Context, runner *system.Runner) error {
	if !fcitxRunning() {
		return nil
	}
	if err := SyncRuntimeConfig(ctx, runner); err != nil {
		return err
	}
	if err := runner.Run(ctx, "fcitx5-remote", "-r"); err != nil {
		return err
	}
	if err := runner.Run(ctx, "dbus-send", "--session", "--dest=org.fcitx.Fcitx5", "/controller", "org.fcitx.Fcitx.Controller1.ReloadConfig"); err != nil {
		return err
	}
	if err := runner.Run(ctx, "fcitx5-remote", "-s", "rime"); err != nil {
		return err
	}
	return nil
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
		return fmt.Errorf("复制默认主题失败: %w", err)
	}

	themeConfPath := filepath.Join(targetDir, "theme.conf")
	content, err := os.ReadFile(themeConfPath)
	if err != nil {
		return fmt.Errorf("读取主题配置失败: %w", err)
	}

	updated := removeAccentColorField(string(content))
	if updated == string(content) {
		return fmt.Errorf("未能从主题配置中移除 AccentColorField，默认主题结构可能已变化")
	}
	if err := system.WriteFileAtomic(themeConfPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("写入自定义主题配置失败: %w", err)
	}
	return nil
}

func removeAccentColorField(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[AccentColorField]" {
			skipping = true
			continue
		}
		if skipping && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			skipping = false
		}
		if skipping {
			continue
		}
		out = append(out, line)
	}

	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

func classicUIRuntimeConfig() string {
	return `<{'Vertical Candidate List': <'False'>, 'PerScreenDPI': <'False'>, 'Font': <'Noto Sans Mono 13'>, 'Theme': <'installer-dark'>, 'DarkTheme': <'installer-dark'>, 'UseDarkTheme': <'False'>, 'UseAccentColor': <'False'>}>`
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
