package fcitx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

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
	fcitxBusName      = "org.fcitx.Fcitx5"
	controllerPath    = "/controller"
	controllerIface   = "org.fcitx.Fcitx.Controller1."
	rimeIface         = "org.fcitx.Fcitx.Rime1."
	sessionBusName    = "org.freedesktop.DBus"
	sessionBusPath    = "/org/freedesktop/DBus"
)

func init() {
	// Fcitx treats spaces before '=' as part of the key.
	ini.PrettyFormat = false
}

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
	if !fcitxRunning(ctx) {
		return nil
	}

	// ReloadConfig does not reliably reload the input method group from profile.
	// Apply it through the controller so the running instance can switch to Rime.
	if _, err := callSessionBus(ctx, runner, fcitxBusName, controllerPath, controllerIface+"AddInputMethodGroup", defaultGroupName); err != nil {
		return err
	}
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

	if _, err := callSessionBus(ctx, runner, fcitxBusName, controllerPath, controllerIface+"Save"); err != nil {
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
	for _, expected := range []string{"Control+space", "Shift_L", "Shift_R", "ModifierOnlyKeyTimeout': <'-1'", "ActiveByDefault': <'True'"} {
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
	if err := cfg.SaveTo(path); err != nil {
		return fmt.Errorf("保存配置失败 %s: %w", path, err)
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
	cfg.Section("Behavior").Key("ActiveByDefault").SetValue("True")

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
	if err := cfg.SaveTo(path); err != nil {
		return fmt.Errorf("保存 Fcitx5 全局配置失败: %w", err)
	}
	return nil
}

func ensureProfile(path string) error {
	cfg, err := ini.LooseLoad(path)
	if err != nil {
		return fmt.Errorf("加载 profile 失败: %w", err)
	}

	groupPath := "Groups/0"
	for _, section := range cfg.Sections() {
		suffix, found := strings.CutPrefix(section.Name(), "Groups/")
		if found && !strings.Contains(suffix, "/") &&
			section.HasKey("Name") && section.Key("Name").String() == defaultGroupName {
			groupPath = section.Name()
			break
		}
	}

	// Keep the shortcuts deterministic by giving the managed group two entries.
	itemsPath := groupPath + "/Items/"
	for _, section := range cfg.Sections() {
		if strings.HasPrefix(section.Name(), itemsPath) {
			cfg.DeleteSection(section.Name())
		}
	}

	group := cfg.Section(groupPath)
	previousGroupName := group.Key("Name").String()
	group.Key("Name").SetValue(defaultGroupName)
	group.Key("Default Layout").SetValue("us")
	// The first item provides the inactive English keyboard. DefaultIM is the
	// input method activated by Ctrl+Space, so it must be Rime rather than the
	// keyboard item.
	group.Key("DefaultIM").SetValue("rime")

	keyboardSection := cfg.Section(itemsPath + "0")
	keyboardSection.Key("Name").SetValue(keyboardUS)
	keyboardSection.Key("Layout").SetValue("")

	rimeSection := cfg.Section(itemsPath + "1")
	rimeSection.Key("Name").SetValue("rime")
	rimeSection.Key("Layout").SetValue("")

	// Keep GroupOrder after the item sections, matching the profile Fcitx writes.
	// A group order section between Groups/0 and its items can leave only the
	// keyboard item in the live group when Fcitx starts.
	orderNames := []string{defaultGroupName}
	if cfg.HasSection("GroupOrder") {
		for _, key := range cfg.Section("GroupOrder").Keys() {
			name := key.Value()
			if name != "" && name != previousGroupName && !slices.Contains(orderNames, name) {
				orderNames = append(orderNames, name)
			}
		}
		cfg.DeleteSection("GroupOrder")
	}
	order := cfg.Section("GroupOrder")
	for index, name := range orderNames {
		order.Key(strconv.Itoa(index)).SetValue(name)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 profile 目录失败: %w", err)
	}
	if err := cfg.SaveTo(path); err != nil {
		return fmt.Errorf("保存 profile 失败: %w", err)
	}
	return nil
}

func Reload(ctx context.Context, runner *system.Runner) error {
	if !fcitxRunning(ctx) {
		return nil
	}
	_, err := callSessionBus(ctx, runner, fcitxBusName, controllerPath, controllerIface+"ReloadConfig")
	return err
}

// ActivateRime loads the deployed data in a fresh Fcitx5 instance and enables
// Rime in the current input context. A non-graphical install can apply the
// on-disk defaults when Fcitx5 first starts.
func ActivateRime(ctx context.Context, runner *system.Runner, schema string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var previousOwner string
	if fcitxRunning(ctx) {
		owner, err := fcitxBusOwner(ctx, runner)
		if err != nil {
			return false, err
		}
		previousOwner = owner
		if _, err := callSessionBus(ctx, runner, owner, controllerPath, controllerIface+"Restart"); err != nil {
			return false, fmt.Errorf("请求重启 Fcitx5 失败: %w", err)
		}
	} else {
		if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") == "" {
			return false, ctx.Err()
		}
		if _, err := callSessionBus(ctx, runner, sessionBusName, sessionBusPath, sessionBusName+".StartServiceByName", fcitxBusName, "0"); err != nil {
			return false, fmt.Errorf("在当前会话启动 Fcitx5 失败: %w", err)
		}
	}

	for {
		owner, err := waitForFcitxBusOwner(ctx, runner, previousOwner)
		if err != nil {
			return false, err
		}
		if err = selectRime(ctx, runner, owner, schema); err == nil {
			_, err = callSessionBus(ctx, runner, owner, controllerPath, controllerIface+"Save")
		}
		// A service manager may stop Fcitx's self-restarted process and
		// start a replacement. Apply the selection to the surviving owner.
		currentOwner, _ := fcitxBusOwner(ctx, runner)
		if currentOwner == owner {
			return err == nil, err
		}
		previousOwner = owner
	}
}

func selectRime(ctx context.Context, runner *system.Runner, owner, schema string) error {
	controller := func(method string, args ...string) (string, error) {
		return callSessionBus(ctx, runner, owner, controllerPath, controllerIface+method, args...)
	}
	rime := func(method string, args ...string) (string, error) {
		return callSessionBus(ctx, runner, owner, "/rime", rimeIface+method, args...)
	}
	if _, err := controller("SwitchInputMethodGroup", defaultGroupName); err != nil {
		return err
	}
	if _, err := controller("SetCurrentIM", "rime"); err != nil {
		return err
	}
	currentIM, err := controller("CurrentInputMethod")
	if err != nil {
		return err
	}
	if currentIM == "('',)" {
		// With no input context, SetCurrentIM sets the group's default. New
		// contexts activate that default through ActiveByDefault.
		group, err := controller("FullInputMethodGroupInfo", defaultGroupName)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(group, "('"+defaultGroupName+"', 'rime',") {
			return fmt.Errorf("Fcitx5 默认输入法未切换为 Rime: %s", group)
		}
		// Load the addon even if the user disabled input method preloading.
		if _, err := controller("GetConfig", "fcitx://config/addon/rime"); err != nil {
			return err
		}
	} else if currentIM != "('rime',)" {
		return fmt.Errorf("Fcitx5 当前输入法未切换为 Rime: %s", currentIM)
	}

	schemas, err := rime("ListAllSchemas")
	if err != nil {
		return err
	}
	if !strings.Contains(schemas, "'"+schema+"'") {
		return fmt.Errorf("Rime 未加载雾凇拼音方案 %s: %s", schema, schemas)
	}
	if currentIM == "('',)" {
		return nil
	}
	// The addon can answer D-Bus calls while librime is still initializing.
	// Its setters have no effect until a Rime session exists.
	if err := waitForRimeSession(ctx, rime); err != nil {
		return err
	}
	if _, err := rime("SetSchema", schema); err != nil {
		return err
	}
	if _, err := rime("SetAsciiMode", "false"); err != nil {
		return err
	}
	currentSchema, err := rime("GetCurrentSchema")
	if err != nil {
		return err
	}
	if currentSchema != "('"+schema+"',)" {
		return fmt.Errorf("Rime 当前方案未切换为雾凇拼音: %s", currentSchema)
	}
	asciiMode, err := rime("IsAsciiMode")
	if err != nil {
		return err
	}
	if asciiMode != "(false,)" {
		return fmt.Errorf("Rime 未切换到中文模式: %s", asciiMode)
	}
	return nil
}

func waitForRimeSession(ctx context.Context, call func(string, ...string) (string, error)) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		schema, err := call("GetCurrentSchema")
		if err != nil {
			return err
		}
		if schema != "('',)" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 Rime 输入会话初始化失败: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForFcitxBusOwner(ctx context.Context, runner *system.Runner, previous string) (string, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		owner, err := fcitxBusOwner(ctx, runner)
		if err == nil && owner != previous {
			return owner, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("等待新的 Fcitx5 实例连接 D-Bus 失败: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func fcitxBusOwner(ctx context.Context, runner *system.Runner) (string, error) {
	output, err := callSessionBus(ctx, runner, sessionBusName, sessionBusPath, sessionBusName+".GetNameOwner", fcitxBusName)
	if err != nil {
		return "", err
	}
	owner := strings.TrimSuffix(strings.TrimPrefix(output, "('"), "',)")
	if !strings.HasPrefix(owner, ":") {
		return "", fmt.Errorf("无法识别 Fcitx5 的 D-Bus 唯一名称: %s", output)
	}
	return owner, nil
}

func callSessionBus(ctx context.Context, runner *system.Runner, destination, path, method string, args ...string) (string, error) {
	command := []string{"call", "--session", "--timeout", "5", "--dest", destination, "--object-path", path, "--method", method}
	return runner.RunCapture(ctx, "gdbus", append(command, args...)...)
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
	return `<{'Hotkey': <{'TriggerKeys': <{'0': <'Control+space'>}>, 'AltTriggerKeys': <@a{sv} {}>, 'EnumerateForwardKeys': <{'0': <'Shift_L'>, '1': <'Shift_R'>}>, 'ModifierOnlyKeyTimeout': <'-1'>}>, 'Behavior': <{'ActiveByDefault': <'True'>}>}>`
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

func fcitxRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "fcitx5-remote", "--check")
	return cmd.Run() == nil
}
