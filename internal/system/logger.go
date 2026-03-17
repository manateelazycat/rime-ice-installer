package system

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	path    string
	file    *os.File
	logger  *log.Logger
	verbose bool
	mu      sync.Mutex
}

func NewLogger(workspace string, verbose bool) (*Logger, error) {
	logDir := filepath.Join(workspace, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("install-%s.log", time.Now().Format("20060102-150405")))
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("创建日志文件失败: %w", err)
	}

	return &Logger{
		path:    logPath,
		file:    file,
		logger:  log.New(file, "", log.LstdFlags),
		verbose: verbose,
	}, nil
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *Logger) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.Println(msg)
	if l.verbose {
		fmt.Println(msg)
	}
}
