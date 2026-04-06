package rime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"rime-ice-installer/internal/config"
	"rime-ice-installer/internal/download"
	"rime-ice-installer/internal/system"
)

const (
	rimeIceOwner = "iDvel"
	rimeIceRepo  = "rime-ice"
	rimeIceTag   = "nightly"
	rimeIceAsset = "full.zip"
)

type PreparedWorkspace struct {
	SourceDir string
	Release   config.ReleaseInfo
}

func PrepareWorkspace(ctx context.Context, client *download.Client, workspace string) (*PreparedWorkspace, error) {
	release, err := client.ReleaseByTag(ctx, rimeIceOwner, rimeIceRepo, rimeIceTag)
	if err != nil {
		return nil, err
	}

	asset, err := download.FindAsset(release, rimeIceAsset)
	if err != nil {
		return nil, err
	}

	downloadPath := filepath.Join(workspace, "downloads", asset.Name)
	if err := client.DownloadAsset(ctx, asset, downloadPath); err != nil {
		return nil, err
	}

	sourceDir := filepath.Join(workspace, "source", "rime-ice")
	if err := system.UnzipFile(downloadPath, sourceDir); err != nil {
		return nil, err
	}

	if err := MergeDefaultCustomConfig(filepath.Join(sourceDir, "default.custom.yaml")); err != nil {
		return nil, err
	}
	if err := EnsureActiveSchema(filepath.Join(sourceDir, "user.yaml")); err != nil {
		return nil, err
	}

	return &PreparedWorkspace{
		SourceDir: sourceDir,
		Release: config.ReleaseInfo{
			Owner:     rimeIceOwner,
			Repo:      rimeIceRepo,
			Tag:       release.TagName,
			AssetName: asset.Name,
			Digest:    normalizeDigest(asset.Digest),
			URL:       asset.BrowserDownloadURL,
		},
	}, nil
}

func MergeDefaultCustomConfig(path string) error {
	root := map[string]any{}
	if content, err := os.ReadFile(path); err == nil {
		if len(strings.TrimSpace(string(content))) > 0 {
			if err := yaml.Unmarshal(content, &root); err != nil {
				return fmt.Errorf("解析现有 default.custom.yaml 失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取 default.custom.yaml 失败: %w", err)
	}

	patch, ok := root["patch"].(map[string]any)
	if !ok || patch == nil {
		patch = map[string]any{}
	}

	patch["menu/page_size"] = 9
	patch["ascii_composer/good_old_caps_lock"] = true
	patch["ascii_composer/switch_key/Shift_L"] = "inline_ascii"
	patch["ascii_composer/switch_key/Shift_R"] = "noop"
	patch["ascii_composer/switch_key/Control_L"] = "noop"
	patch["ascii_composer/switch_key/Control_R"] = "noop"
	patch["key_binder/bindings/+"] = []any{
		map[string]any{
			"when":   "has_menu",
			"accept": "comma",
			"send":   "Page_Up",
		},
		map[string]any{
			"when":   "has_menu",
			"accept": "period",
			"send":   "Page_Down",
		},
	}

	root["patch"] = patch
	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("序列化 default.custom.yaml 失败: %w", err)
	}

	return system.WriteFileAtomic(path, out, 0o644)
}

func Deploy(sourceDir string, targets []string) (*config.DeploymentResult, error) {
	result := &config.DeploymentResult{
		BackupPaths: make([]string, 0, len(targets)),
		TargetPaths: make([]string, 0, len(targets)),
	}

	for _, target := range targets {
		backupPath, created, err := system.BackupDirWithSuffix(target, "_bak")
		if err != nil {
			return nil, err
		}
		if created {
			result.BackupPaths = append(result.BackupPaths, backupPath)
		}

		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, fmt.Errorf("创建目标目录失败: %w", err)
		}
		if err := os.RemoveAll(target); err != nil {
			return nil, fmt.Errorf("清理目标目录失败: %w", err)
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, fmt.Errorf("重建目标目录失败: %w", err)
		}
		if err := system.CopyDir(sourceDir, target); err != nil {
			return nil, fmt.Errorf("复制雾凇配置失败: %w", err)
		}
		result.TargetPaths = append(result.TargetPaths, target)
	}

	return result, nil
}

func EnsureActiveSchema(path string) error {
	root := map[string]any{}
	if content, err := os.ReadFile(path); err == nil {
		if len(strings.TrimSpace(string(content))) > 0 {
			if err := yaml.Unmarshal(content, &root); err != nil {
				return fmt.Errorf("解析 user.yaml 失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取 user.yaml 失败: %w", err)
	}

	varSection, ok := root["var"].(map[string]any)
	if !ok || varSection == nil {
		varSection = map[string]any{}
	}
	varSection["previously_selected_schema"] = "rime_ice"
	root["var"] = varSection

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("序列化 user.yaml 失败: %w", err)
	}
	return system.WriteFileAtomic(path, out, 0o644)
}

func Build(ctx context.Context, runner *system.Runner, userDir string) error {
	buildDir := filepath.Join(userDir, "build")
	return runner.Run(ctx, "rime_deployer", "--build", userDir, "/usr/share/rime-data", buildDir)
}

func normalizeDigest(digest string) string {
	return strings.TrimPrefix(digest, "sha256:")
}
