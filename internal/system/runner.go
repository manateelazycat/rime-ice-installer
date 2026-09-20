package system

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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

func (r *Runner) ValidateCachedSudo(ctx context.Context) bool {
	if os.Geteuid() == 0 || r.DryRun {
		return true
	}
	_, err := r.run(ctx, nil, "sudo", "-n", "-v")
	return err == nil
}

func (r *Runner) ValidateSudoFingerprint(ctx context.Context) error {
	if os.Geteuid() == 0 || r.DryRun {
		return nil
	}

	// sudo must perform the PAM conversation itself. Keep stdin open while
	// pam_fprintd is active, then close it as soon as PAM falls through to the
	// password prompt. This avoids submitting an empty password and incrementing
	// pam_faillock after a missed fingerprint.
	const passwordPrompt = "rime-ice-installer-password-fallback:"
	if r.Logger != nil {
		r.Logger.Printf("$ sudo -S -p %q -v", passwordPrompt)
	}

	cmd := exec.CommandContext(ctx, "sudo", "-S", "-p", passwordPrompt, "-v")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建 sudo 输入管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("创建 sudo 输出管道失败: %w", err)
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("启动 sudo 指纹验证失败: %w", err)
	}

	var closeInput sync.Once
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, 256)
		var pending string
		for {
			n, readErr := stderr.Read(buffer)
			if n > 0 {
				pending += string(buffer[:n])
				if strings.Contains(pending, passwordPrompt) {
					closeInput.Do(func() { _ = stdin.Close() })
				}
				if len(pending) > len(passwordPrompt)*2 {
					pending = pending[len(pending)-len(passwordPrompt)*2:]
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	closeInput.Do(func() { _ = stdin.Close() })
	<-readDone
	if waitErr != nil {
		return fmt.Errorf("sudo 指纹验证失败: %w", waitErr)
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
