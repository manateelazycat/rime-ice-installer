package system

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Runner struct {
	DryRun       bool
	Logger       *Logger
	sudoPassword string
}

func NewRunner(dryRun bool, logger *Logger) *Runner {
	return &Runner{
		DryRun: dryRun,
		Logger: logger,
	}
}

func (r *Runner) SetSudoPassword(password string) {
	r.sudoPassword = password
}

func (r *Runner) ValidateSudo(ctx context.Context) error {
	if os.Geteuid() == 0 || r.DryRun {
		return nil
	}

	if r.sudoPassword != "" {
		_, err := r.runWithInput(ctx, nil, r.sudoPassword+"\n", "sudo", "-S", "-p", "", "-v")
		if err != nil {
			return fmt.Errorf("sudo 验证失败: %w", err)
		}
		return nil
	}

	if _, err := r.run(ctx, nil, "sudo", "-v"); err != nil {
		return fmt.Errorf("sudo 验证失败: %w", err)
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, name string, args ...string) error {
	_, err := r.run(ctx, nil, name, args...)
	return err
}

func (r *Runner) RunCapture(ctx context.Context, name string, args ...string) (string, error) {
	return r.run(ctx, nil, name, args...)
}

func (r *Runner) RunPrivileged(ctx context.Context, name string, args ...string) error {
	if os.Geteuid() == 0 {
		return r.Run(ctx, name, args...)
	}
	if r.sudoPassword != "" {
		allArgs := append([]string{"-S", "-p", "", name}, args...)
		_, err := r.runWithInput(ctx, nil, r.sudoPassword+"\n", "sudo", allArgs...)
		return err
	}
	allArgs := append([]string{name}, args...)
	return r.Run(ctx, "sudo", allArgs...)
}

func (r *Runner) run(ctx context.Context, extraEnv []string, name string, args ...string) (string, error) {
	return r.runWithInput(ctx, extraEnv, "", name, args...)
}

func (r *Runner) runWithInput(ctx context.Context, extraEnv []string, input string, name string, args ...string) (string, error) {
	commandLine := shellQuote(name, args...)
	if r.Logger != nil {
		r.Logger.Printf("$ %s", commandLine)
	}
	if r.DryRun {
		return "", nil
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(combined.String())
		if r.Logger != nil && output != "" {
			r.Logger.Printf("%s", output)
		}
		if output == "" {
			return "", fmt.Errorf("命令执行失败: %s: %w", commandLine, err)
		}
		return "", fmt.Errorf("命令执行失败: %s: %w\n%s", commandLine, err, output)
	}

	output := strings.TrimSpace(combined.String())
	if r.Logger != nil && output != "" {
		r.Logger.Printf("%s", output)
	}
	return output, nil
}

func shellQuote(name string, args ...string) string {
	all := append([]string{name}, args...)
	quoted := make([]string, 0, len(all))
	for _, item := range all {
		if item == "" {
			quoted = append(quoted, "''")
			continue
		}
		if strings.IndexFunc(item, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"'
		}) >= 0 {
			quoted = append(quoted, fmt.Sprintf("%q", item))
			continue
		}
		quoted = append(quoted, item)
	}
	return strings.Join(quoted, " ")
}
