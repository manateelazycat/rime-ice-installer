package wanxiang

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
	modelOwner = "amzxyz"
	modelRepo  = "RIME-LMDG"
	modelTag   = "LTS"
	modelAsset = "wanxiang-lts-zh-hans.gram"
)

func Integrate(ctx context.Context, client *download.Client, workspace, sourceDir string) (*config.ReleaseInfo, error) {
	release, err := client.ReleaseByTag(ctx, modelOwner, modelRepo, modelTag)
	if err != nil {
		return nil, err
	}

	asset, err := download.FindAsset(release, modelAsset)
	if err != nil {
		return nil, err
	}

	downloadPath := filepath.Join(workspace, "downloads", asset.Name)
	if err := client.DownloadAsset(ctx, asset, downloadPath); err != nil {
		return nil, err
	}

	targetModelPath := filepath.Join(sourceDir, asset.Name)
	modelContent, err := os.ReadFile(downloadPath)
	if err != nil {
		return nil, fmt.Errorf("读取模型文件失败: %w", err)
	}
	if err := system.WriteFileAtomic(targetModelPath, modelContent, 0o644); err != nil {
		return nil, err
	}

	if err := MergeCustomConfig(filepath.Join(sourceDir, "rime_ice.custom.yaml")); err != nil {
		return nil, err
	}

	return &config.ReleaseInfo{
		Owner:     modelOwner,
		Repo:      modelRepo,
		Tag:       release.TagName,
		AssetName: asset.Name,
		Digest:    strings.TrimPrefix(asset.Digest, "sha256:"),
		URL:       asset.BrowserDownloadURL,
	}, nil
}

func MergeCustomConfig(path string) error {
	root := map[string]any{}
	if content, err := os.ReadFile(path); err == nil {
		if len(strings.TrimSpace(string(content))) > 0 {
			if err := yaml.Unmarshal(content, &root); err != nil {
				return fmt.Errorf("解析现有 custom yaml 失败: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取 custom yaml 失败: %w", err)
	}

	patch, ok := root["patch"].(map[string]any)
	if !ok || patch == nil {
		patch = map[string]any{}
	}

	patch["grammar/language"] = "wanxiang-lts-zh-hans"
	patch["grammar/collocation_max_length"] = 7
	patch["grammar/collocation_min_length"] = 2
	patch["grammar/collocation_penalty"] = -10
	patch["grammar/non_collocation_penalty"] = -20
	patch["grammar/weak_collocation_penalty"] = -35
	patch["grammar/rear_penalty"] = -12
	patch["translator/contextual_suggestions"] = false
	patch["translator/max_homophones"] = 5
	patch["translator/max_homographs"] = 5

	root["patch"] = patch
	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("序列化 custom yaml 失败: %w", err)
	}

	if err := system.WriteFileAtomic(path, out, 0o644); err != nil {
		return err
	}
	return nil
}
