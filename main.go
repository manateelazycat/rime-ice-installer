package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"rime-ice-installer/internal/app"
	"rime-ice-installer/internal/config"
)

func main() {
	cfg := config.DefaultInstallConfig()

	flag.BoolVar(&cfg.Yes, "yes", false, "跳过 TUI 交互并直接按当前选项执行")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "只展示执行计划，不做实际修改")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "输出更详细的日志")
	flag.BoolVar(&cfg.EnableWanxiang, "enable-wanxiang", cfg.EnableWanxiang, "下载并启用万象 LTS 语法模型")
	flag.StringVar(&cfg.WorkspaceDir, "workspace-dir", cfg.WorkspaceDir, "工作目录，用于下载缓存和日志")
	flag.Parse()

	if err := app.Run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
