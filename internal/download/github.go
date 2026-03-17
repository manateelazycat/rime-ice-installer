package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rime-ice-installer/internal/system"
)

type GitHubRelease struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	Assets  []GitHubAsset `json:"assets"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Client struct {
	httpClient *http.Client
	logger     *system.Logger
}

func NewClient(logger *system.Logger) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Minute},
		logger:     logger,
	}
}

func (c *Client) ReleaseByTag(ctx context.Context, owner, repo, tag string) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rime-ice-installer")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub Release 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("GitHub Release 请求失败: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析 GitHub Release 失败: %w", err)
	}
	return &release, nil
}

func FindAsset(release *GitHubRelease, names ...string) (*GitHubAsset, error) {
	for _, expected := range names {
		for _, asset := range release.Assets {
			if asset.Name == expected {
				matched := asset
				return &matched, nil
			}
		}
	}
	return nil, fmt.Errorf("未找到资产: %s", strings.Join(names, ", "))
}

func (c *Client) DownloadAsset(ctx context.Context, asset *GitHubAsset, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}

	expectedDigest := normalizeDigest(asset.Digest)
	if expectedDigest != "" {
		if currentDigest, err := system.ComputeSHA256(destPath); err == nil && currentDigest == expectedDigest {
			if c.logger != nil {
				c.logger.Printf("复用已下载文件: %s", destPath)
			}
			return nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("创建下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "rime-ice-installer")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载资产失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("下载资产失败: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	tmpPath := destPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("创建临时下载文件失败: %w", err)
	}

	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("写入下载文件失败: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("关闭下载文件失败: %w", err)
	}

	if expectedDigest != "" {
		actualDigest, err := system.ComputeSHA256(tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		if actualDigest != expectedDigest {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("下载文件 sha256 校验失败: 期望 %s, 实际 %s", expectedDigest, actualDigest)
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("保存下载文件失败: %w", err)
	}
	return nil
}

func normalizeDigest(digest string) string {
	if digest == "" {
		return ""
	}
	if strings.HasPrefix(digest, "sha256:") {
		return strings.TrimPrefix(digest, "sha256:")
	}
	return digest
}
