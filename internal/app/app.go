package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rime-ice-installer/internal/config"
	"rime-ice-installer/internal/download"
	"rime-ice-installer/internal/env"
	"rime-ice-installer/internal/fcitx"
	"rime-ice-installer/internal/rime"
	"rime-ice-installer/internal/system"
	"rime-ice-installer/internal/ui"
	"rime-ice-installer/internal/wanxiang"
)

const maxSudoAttempts = 3

func Run(ctx context.Context, cfg config.InstallConfig) error {
	detectedEnv, err := env.Detect()
	if err != nil {
		return err
	}

	var dialogUI *ui.Dialog
	if !cfg.Yes {
		if !detectedEnv.DialogAvailable {
			return fmt.Errorf("未检测到 dialog，无法进入 TUI；可使用 --yes 走非交互模式")
		}
		dialogUI = ui.NewDialog()
		defer ui.RestoreTerminal()
		cfg, err = dialogUI.CollectOptions(cfg, detectedEnv)
		if err != nil {
			if errors.Is(err, ui.ErrCancelled) {
				return nil
			}
			return err
		}
	}

	if err := validateConfig(cfg, detectedEnv); err != nil {
		if dialogUI != nil {
			dialogUI.ShowError(err.Error())
		}
		return err
	}

	summary := buildSummary(cfg, detectedEnv)
	if dialogUI != nil {
		confirmed, err := dialogUI.ConfirmPlan(summary)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	} else {
		fmt.Println(summary)
	}

	if cfg.DryRun {
		if dialogUI != nil {
			return dialogUI.MsgBox("Dry Run 完成", "已展示执行计划，未做任何修改。")
		}
		fmt.Println("\nDry run 完成，未做任何修改。")
		return nil
	}

	logger, err := system.NewLogger(cfg.WorkspaceDir, cfg.Verbose)
	if err != nil {
		return err
	}
	defer logger.Close()

	runner := system.NewRunner(cfg.DryRun, logger)
	downloader := download.NewClient(logger)

	if err := system.EnsureDir(cfg.WorkspaceDir, 0o755); err != nil {
		return err
	}

	if os.Geteuid() != 0 {
		if dialogUI != nil {
			if err := validateSudoWithDialog(ctx, runner, dialogUI); err != nil {
				if errors.Is(err, ui.ErrCancelled) {
					return nil
				}
				return err
			}
		} else if err := runner.ValidateSudo(ctx); err != nil {
			return err
		}
	}

	startedAt := time.Now()
	var envFile string
	var configuredFiles []string
	var rimeWorkspace *rime.PreparedWorkspace
	var modelRelease *config.ReleaseInfo
	var deployResult *config.DeploymentResult
	var gauge *ui.Gauge

	totalSteps := 7
	if cfg.EnableWanxiang {
		totalSteps++
	}
	if dialogUI != nil {
		gauge, err = dialogUI.StartGauge("安装中", "准备开始安装...")
		if err != nil {
			gauge = nil
			logger.Printf("无法启动进度条，退回日志模式: %v", err)
		}
	}
	currentStep := 0

	step := func(name string, fn func() error) error {
		currentStep++
		startPercent := ((currentStep - 1) * 100) / totalSteps
		endPercent := (currentStep * 100) / totalSteps
		stepMessage := fmt.Sprintf("[%d/%d] %s", currentStep, totalSteps, name)
		if gauge != nil {
			_ = gauge.Update(max(1, startPercent), stepMessage)
		}
		logger.Printf("开始步骤: %s", name)
		begin := time.Now()
		if err := fn(); err != nil {
			logger.Printf("步骤失败: %s: %v", name, err)
			return fmt.Errorf("%s: %w", name, err)
		}
		logger.Printf("步骤完成: %s (耗时 %s)", name, time.Since(begin).Round(time.Second))
		if gauge != nil {
			_ = gauge.Update(endPercent, stepMessage+"：完成")
		}
		return nil
	}

	if err := step("安装 Fcitx5 与依赖", func() error {
		return fcitx.InstallPackages(ctx, runner, cfg, detectedEnv)
	}); err != nil {
		closeGauge(gauge)
		return fail(dialogUI, logger, err)
	}

	if err := step("验证 Fcitx5 Rime 运行库", func() error {
		return fcitx.ValidateRimeRuntime(ctx, runner)
	}); err != nil {
		closeGauge(gauge)
		return fail(dialogUI, logger, err)
	}

	if err := step("写入 IM 环境变量", func() error {
		var err error
		envFile, err = fcitx.EnsureIMEEnvironment(detectedEnv)
		return err
	}); err != nil {
		closeGauge(gauge)
		return fail(dialogUI, logger, err)
	}

	if err := step("写入 Fcitx5 用户配置", func() error {
		var err error
		configuredFiles, err = fcitx.Configure(detectedEnv.HomeDir)
		if err != nil {
			return err
		}
		return fcitx.SyncRuntimeConfig(ctx, runner)
	}); err != nil {
		closeGauge(gauge)
		return fail(dialogUI, logger, err)
	}

	if err := step("下载并补丁雾凇拼音", func() error {
		var err error
		rimeWorkspace, err = rime.PrepareWorkspace(ctx, downloader, cfg.WorkspaceDir)
		return err
	}); err != nil {
		closeGauge(gauge)
		return fail(dialogUI, logger, err)
	}

	if cfg.EnableWanxiang {
		if err := step("下载并启用万象语法模型", func() error {
			var err error
			modelRelease, err = wanxiang.Integrate(ctx, downloader, cfg.WorkspaceDir, rimeWorkspace.SourceDir)
			return err
		}); err != nil {
			closeGauge(gauge)
			return fail(dialogUI, logger, err)
		}
	}

	if err := step("备份并部署 Rime 配置", func() error {
		targets := []string{
			filepath.Join(detectedEnv.HomeDir, ".config", "fcitx", "rime"),
			filepath.Join(detectedEnv.HomeDir, ".local", "share", "fcitx5", "rime"),
		}
		var err error
		deployResult, err = rime.Deploy(rimeWorkspace.SourceDir, targets)
		return err
	}); err != nil {
		closeGauge(gauge)
		return fail(dialogUI, logger, err)
	}

	if err := step("编译并激活 Rime", func() error {
		userDir := filepath.Join(detectedEnv.HomeDir, ".local", "share", "fcitx5", "rime")
		if err := rime.Build(ctx, runner, userDir); err != nil {
			return err
		}
		return fcitx.ReloadAndActivate(ctx, runner)
	}); err != nil {
		closeGauge(gauge)
		return fail(dialogUI, logger, err)
	}

	closeGauge(gauge)

	result := renderResult(startedAt, logger.Path(), envFile, configuredFiles, rimeWorkspace, modelRelease, deployResult, cfg, detectedEnv)
	if dialogUI != nil {
		if err := dialogUI.MsgBox("安装完成", result); err != nil {
			return err
		}
		if err := fcitx.Restart(ctx, runner); err != nil {
			dialogUI.ShowError(fmt.Sprintf("Fcitx 自动重启失败: %v", err))
			return err
		}
	} else {
		fmt.Println(result)
		if err := fcitx.Restart(ctx, runner); err != nil {
			return err
		}
	}
	return nil
}

func validateConfig(cfg config.InstallConfig, detectedEnv config.DetectedEnv) error {
	missing := env.MissingCommands(env.RequiredCommands(!cfg.Yes))
	if len(missing) > 0 {
		return fmt.Errorf("缺少必要命令: %s", strings.Join(missing, ", "))
	}
	return nil
}

func buildSummary(cfg config.InstallConfig, env config.DetectedEnv) string {
	lines := []string{
		"将要执行以下操作：",
		"",
		"1. 安装 Fcitx5、GTK/Qt 模块、配置工具、Rime、librime 和 opencc",
		"2. 写入并同步 Fcitx5 主题与快捷键清理配置（默认主题为 installer-dark）",
		fmt.Sprintf("3. 写入 IM 环境变量文件：%s", env.EnvironmentFilePath),
		"4. 下载 rime-ice nightly 并修改 default.yaml",
		"5. 备份并覆盖以下目录：",
		"   - ~/.config/fcitx/rime -> 同级 _bak",
		"   - ~/.local/share/fcitx5/rime -> 同级 _bak",
		"6. 编译并激活 rime_ice 方案",
	}
	if cfg.EnableWanxiang {
		lines = append(lines, "7. 下载并启用 wanxiang-lts-zh-hans.gram")
	}
	lines = append(lines,
		"",
		fmt.Sprintf("当前会话: %s", blankIfUnknown(env.SessionType)),
		fmt.Sprintf("当前桌面: %s", blankIfUnknown(env.Desktop)),
		"",
		"继续执行吗？",
	)
	return strings.Join(lines, "\n")
}

func renderResult(startedAt time.Time, logPath, envFile string, configuredFiles []string, rimeWorkspace *rime.PreparedWorkspace, modelRelease *config.ReleaseInfo, deployResult *config.DeploymentResult, cfg config.InstallConfig, detectedEnv config.DetectedEnv) string {
	lines := []string{
		fmt.Sprintf("安装完成，耗时 %s。", time.Since(startedAt).Round(time.Second)),
		"",
		fmt.Sprintf("日志: %s", logPath),
		fmt.Sprintf("环境变量文件: %s", envFile),
	}

	if len(configuredFiles) > 0 {
		lines = append(lines, "Fcitx5 配置文件:")
		for _, path := range configuredFiles {
			lines = append(lines, "  - "+path)
		}
	}

	if deployResult != nil {
		lines = append(lines, "Rime 备份目录:")
		for _, path := range deployResult.BackupPaths {
			lines = append(lines, "  - "+path)
		}
		lines = append(lines, "Rime 目标目录:")
		for _, path := range deployResult.TargetPaths {
			lines = append(lines, "  - "+path)
		}
	}

	if rimeWorkspace != nil {
		lines = append(lines, fmt.Sprintf("雾凇来源: %s/%s @ %s (%s)", rimeWorkspace.Release.Owner, rimeWorkspace.Release.Repo, rimeWorkspace.Release.Tag, rimeWorkspace.Release.AssetName))
	}
	if cfg.EnableWanxiang && modelRelease != nil {
		lines = append(lines, fmt.Sprintf("万象模型: %s/%s @ %s (%s)", modelRelease.Owner, modelRelease.Repo, modelRelease.Tag, modelRelease.AssetName))
	}

	lines = append(lines,
		"",
		"已自动清空这些多余快捷键：",
		"  - clipboard TriggerKey / PastePrimaryKey",
		"  - quickphrase TriggerKey",
		"  - unicode TriggerKey",
		"",
		"后续操作：",
		"  - 重新登录或重启图形会话",
	)

	if detectedEnv.SessionType == "wayland" && detectedEnv.IsKDE {
		lines = append(lines, "  - 在 KDE 设置 -> 虚拟键盘 中选择 Fcitx5")
	}
	lines = append(lines, "  - 云插件与万象模型不要同时启用")

	return strings.Join(lines, "\n")
}

func fail(dialogUI *ui.Dialog, logger *system.Logger, err error) error {
	if dialogUI != nil {
		dialogUI.ShowError(fmt.Sprintf("%v\n\n日志: %s", err, logger.Path()))
	}
	return err
}

func validateSudoWithDialog(ctx context.Context, runner *system.Runner, dialogUI *ui.Dialog) error {
	for attempt := 1; attempt <= maxSudoAttempts; attempt++ {
		title := fmt.Sprintf("sudo 验证 (%d/%d)", attempt, maxSudoAttempts)
		prompt := "请输入 sudo 密码以继续安装："
		if attempt > 1 {
			prompt = fmt.Sprintf("密码错误，请重新输入 sudo 密码：\n\n剩余尝试次数：%d", maxSudoAttempts-attempt+1)
		}

		password, err := dialogUI.Password(title, prompt)
		if err != nil {
			return err
		}
		runner.SetSudoPassword(password)

		if err := runner.ValidateSudo(ctx); err == nil {
			return nil
		} else if attempt == maxSudoAttempts {
			dialogUI.ShowError(fmt.Sprintf("sudo 密码连续 %d 次验证失败，安装已退出。", maxSudoAttempts))
			return err
		}
	}

	return fmt.Errorf("sudo 验证失败")
}

func closeGauge(gauge *ui.Gauge) {
	if gauge != nil {
		_ = gauge.Close()
	}
}

func blankIfUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未识别"
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
