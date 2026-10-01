package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

const maxDownloadAttempts = 4

type Client struct {
	httpClient *http.Client
	logger     *system.Logger
}

func NewClient(logger *system.Logger) *Client {
	return &Client{
		httpClient: &http.Client{},
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

	tmpPath := destPath + ".tmp"
	if expectedDigest != "" {
		// A release tag can point to a new asset with the same name.
		tmpPath += "." + expectedDigest
	}
	if expectedDigest != "" {
		if info, err := os.Stat(tmpPath); err == nil && (asset.Size == 0 || info.Size() == asset.Size) {
			if digest, err := system.ComputeSHA256(tmpPath); err == nil && digest == expectedDigest {
				if err := os.Rename(tmpPath, destPath); err != nil {
					return fmt.Errorf("保存下载文件失败: %w", err)
				}
				return nil
			}
			if asset.Size > 0 {
				if err := os.Remove(tmpPath); err != nil {
					return fmt.Errorf("删除损坏的临时下载文件失败: %w", err)
				}
			}
		}
	}
	var lastErr error
	for attempt := 1; attempt <= maxDownloadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 1 {
			if c.logger != nil {
				c.logger.Printf("下载中断，重试 %d/%d: %v", attempt, maxDownloadAttempts, lastErr)
			}
			timer := time.NewTimer(time.Duration(1<<(attempt-2)) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		lastErr = c.downloadAttempt(ctx, asset, tmpPath)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return fmt.Errorf("下载资产失败（已尝试 %d 次，临时文件保留在 %s）: %w", maxDownloadAttempts, tmpPath, lastErr)
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

func (c *Client) downloadAttempt(ctx context.Context, asset *GitHubAsset, tmpPath string) error {
	var offset int64
	if info, err := os.Stat(tmpPath); err == nil {
		offset = info.Size()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查临时下载文件失败: %w", err)
	}
	if asset.Size > 0 && offset > asset.Size {
		offset = 0
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("创建下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "rime-ice-installer")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// A server may ignore Range; in that case start over rather than append.
		offset = 0
	case http.StatusPartialContent:
		start, err := contentRangeStart(resp.Header.Get("Content-Range"))
		if err != nil || start != offset {
			return fmt.Errorf("无效的 Content-Range: %q", resp.Header.Get("Content-Range"))
		}
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("创建临时下载文件失败: %w", err)
	}
	if err := file.Truncate(offset); err != nil {
		file.Close()
		return fmt.Errorf("调整临时下载文件失败: %w", err)
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		file.Close()
		return fmt.Errorf("定位临时下载文件失败: %w", err)
	}
	written, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("写入下载文件失败: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭下载文件失败: %w", closeErr)
	}
	if asset.Size > 0 && offset+written != asset.Size {
		return fmt.Errorf("下载文件大小不符: 期望 %d 字节, 当前 %d 字节", asset.Size, offset+written)
	}
	return nil
}

func contentRangeStart(header string) (int64, error) {
	if !strings.HasPrefix(header, "bytes ") {
		return 0, fmt.Errorf("缺少 bytes 前缀")
	}
	start, rest, ok := strings.Cut(strings.TrimPrefix(header, "bytes "), "-")
	if !ok || !strings.Contains(rest, "/") {
		return 0, fmt.Errorf("范围格式错误")
	}
	return strconv.ParseInt(start, 10, 64)
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
