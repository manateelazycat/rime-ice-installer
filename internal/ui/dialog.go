package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"rime-ice-installer/internal/config"
)

var ErrCancelled = errors.New("用户取消操作")

type Dialog struct {
	Backtitle string
}

type Gauge struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	closed bool
}

func NewDialog() *Dialog {
	return &Dialog{Backtitle: "rime-ice-installer"}
}

func (d *Dialog) CollectOptions(cfg config.InstallConfig, env config.DetectedEnv) (config.InstallConfig, error) {
	intro := fmt.Sprintf("检测到当前环境:\n\n会话类型: %s\n桌面环境: %s\nKDE Plasma: %t\n将应用主题: installer-dark（黑色科幻）\n\n接下来选择要安装的组件。",
		blankIfUnknown(env.SessionType),
		blankIfUnknown(env.Desktop),
		env.IsKDE,
	)
	if err := d.MsgBox("环境检测", intro); err != nil {
		return cfg, err
	}

	choices, err := d.checklist("安装选项", "选择需要执行的组件：", []checklistItem{
		{Tag: "wanxiang", Label: "启用万象 LTS 语法模型", Checked: cfg.EnableWanxiang},
	})
	if err != nil {
		return cfg, err
	}

	selected := map[string]bool{}
	for _, choice := range choices {
		selected[choice] = true
	}
	cfg.EnableWanxiang = selected["wanxiang"]

	fontSize, err := d.rangeBox(
		"候选框字号",
		"调节 Fcitx5 输入法候选框的字体大小：",
		config.MinFontSize,
		config.MaxFontSize,
		cfg.FontSize,
	)
	if err != nil {
		return cfg, err
	}
	cfg.FontSize = fontSize
	return cfg, nil
}

const (
	SudoAuthPassword    = "password"
	SudoAuthFingerprint = "fingerprint"
)

func (d *Dialog) ChooseSudoAuth() (string, error) {
	return d.menu("sudo 身份验证", "选择验证方式：", []menuItem{
		{Tag: SudoAuthFingerprint, Label: "使用已录入的指纹"},
		{Tag: SudoAuthPassword, Label: "输入 sudo 密码"},
	})
}

func (d *Dialog) ConfirmPlan(summary string) (bool, error) {
	err := d.runInteractive("确认执行", "--yesno", summary, "22", "88")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrCancelled) {
		return false, nil
	}
	return false, err
}

func (d *Dialog) MsgBox(title, message string) error {
	return d.runInteractive(title, "--msgbox", message, "20", "88")
}

func (d *Dialog) ShowError(message string) {
	_ = d.MsgBox("执行失败", message)
}

func (d *Dialog) Password(title, prompt string) (string, error) {
	args := []string{"--clear", "--stdout", "--insecure", "--backtitle", d.Backtitle, "--title", title, "--passwordbox", prompt, "10", "88"}
	cmd := exec.Command("dialog", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	defer RestoreTerminal()

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if isDialogCancelled(err) {
			return "", ErrCancelled
		}
		return "", err
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func (d *Dialog) StartGauge(title, message string) (*Gauge, error) {
	args := []string{"--clear", "--backtitle", d.Backtitle, "--title", title, "--gauge", message, "10", "88", "0"}
	cmd := exec.Command("dialog", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}

	gauge := &Gauge{
		cmd:   cmd,
		stdin: stdin,
	}
	if err := gauge.Update(1, message); err != nil {
		gauge.Close()
		return nil, err
	}
	return gauge, nil
}

type checklistItem struct {
	Tag     string
	Label   string
	Checked bool
}

type menuItem struct {
	Tag   string
	Label string
}

func (d *Dialog) menu(title, text string, items []menuItem) (string, error) {
	args := []string{"--clear", "--stdout", "--backtitle", d.Backtitle, "--title", title, "--menu", text, "14", "72", "6"}
	for _, item := range items {
		args = append(args, item.Tag, item.Label)
	}

	cmd := exec.Command("dialog", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	defer RestoreTerminal()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if isDialogCancelled(err) {
			return "", ErrCancelled
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (d *Dialog) rangeBox(title, text string, minimum, maximum, current int) (int, error) {
	args := []string{
		"--clear", "--stdout", "--backtitle", d.Backtitle, "--title", title,
		"--rangebox", text, "10", "88",
		fmt.Sprintf("%d", minimum), fmt.Sprintf("%d", maximum), fmt.Sprintf("%d", current),
	}
	cmd := exec.Command("dialog", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	defer RestoreTerminal()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if isDialogCancelled(err) {
			return 0, ErrCancelled
		}
		return 0, err
	}

	var value int
	if _, err := fmt.Sscanf(strings.TrimSpace(stdout.String()), "%d", &value); err != nil {
		return 0, fmt.Errorf("读取字号失败: %w", err)
	}
	return value, nil
}

func (d *Dialog) checklist(title, text string, items []checklistItem) ([]string, error) {
	args := []string{"--clear", "--stdout", "--separate-output", "--backtitle", d.Backtitle, "--title", title, "--checklist", text, "20", "88", "8"}
	for _, item := range items {
		state := "off"
		if item.Checked {
			state = "on"
		}
		args = append(args, item.Tag, item.Label, state)
	}

	cmd := exec.Command("dialog", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	defer RestoreTerminal()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		if isDialogCancelled(err) {
			return nil, ErrCancelled
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}

func (d *Dialog) runInteractive(title string, extraArgs ...string) error {
	args := []string{"--clear", "--backtitle", d.Backtitle, "--title", title}
	args = append(args, extraArgs...)
	cmd := exec.Command("dialog", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	defer RestoreTerminal()
	if err := cmd.Run(); err != nil {
		if isDialogCancelled(err) {
			return ErrCancelled
		}
		return err
	}
	return nil
}

func (g *Gauge) Update(percent int, message string) error {
	if g == nil || g.closed {
		return nil
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	_, err := fmt.Fprintf(g.stdin, "XXX\n%d\n%s\nXXX\n", percent, message)
	return err
}

func (g *Gauge) Close() error {
	if g == nil || g.closed {
		return nil
	}
	g.closed = true
	_ = g.stdin.Close()
	err := g.cmd.Wait()
	RestoreTerminal()
	return err
}

func isDialogCancelled(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return code == 1 || code == 255
	}
	return false
}

func blankIfUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未识别"
	}
	return value
}

func RestoreTerminal() {
	fmt.Fprint(os.Stdout, "\033[?1049l\033[0m\033[39;49m\033[2J\033[H")
	_ = exec.Command("stty", "sane").Run()
	_ = exec.Command("tput", "rmcup").Run()
	_ = exec.Command("tput", "sgr0").Run()
	_ = exec.Command("clear").Run()
}
