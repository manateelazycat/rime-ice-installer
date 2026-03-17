package system

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func EnsureDir(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("替换目标文件失败: %w", err)
	}
	return nil
}

func ReplaceOrAppendBlock(content, startMarker, endMarker, block string) string {
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start >= 0 && end >= 0 && end > start {
		end += len(endMarker)
		replaced := content[:start] + block + content[end:]
		return strings.TrimRight(replaced, "\n") + "\n"
	}

	content = strings.TrimRight(content, "\n")
	if content == "" {
		return block
	}
	return content + "\n\n" + block
}

func ComputeSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("计算 sha256 失败: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func UnzipFile(zipPath, targetDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer reader.Close()

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("清理旧解压目录失败: %w", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建解压目录失败: %w", err)
	}

	for _, file := range reader.File {
		targetPath := filepath.Join(targetDir, file.Name)
		cleanPath := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanPath, filepath.Clean(targetDir)+string(os.PathSeparator)) && cleanPath != filepath.Clean(targetDir) {
			return fmt.Errorf("压缩包路径非法: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanPath, file.Mode()); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
			return fmt.Errorf("创建父目录失败: %w", err)
		}

		src, err := file.Open()
		if err != nil {
			return fmt.Errorf("打开压缩包条目失败: %w", err)
		}

		dst, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
		if err != nil {
			src.Close()
			return fmt.Errorf("创建目标文件失败: %w", err)
		}

		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			src.Close()
			return fmt.Errorf("解压文件失败: %w", err)
		}
		dst.Close()
		src.Close()
	}

	return nil
}

func BackupDirWithSuffix(path, suffix string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path + suffix, false, nil
		}
		return "", false, fmt.Errorf("读取目录状态失败: %w", err)
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("目标不是目录: %s", path)
	}

	backupPath := path + suffix
	if err := os.RemoveAll(backupPath); err != nil {
		return "", false, fmt.Errorf("删除旧备份失败: %w", err)
	}
	if err := os.Rename(path, backupPath); err != nil {
		return "", false, fmt.Errorf("创建备份失败: %w", err)
	}
	return backupPath, true, nil
}

func CopyDir(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}

		targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			sourceFile.Close()
			return err
		}

		if _, err := io.Copy(targetFile, sourceFile); err != nil {
			targetFile.Close()
			sourceFile.Close()
			return err
		}
		if err := targetFile.Close(); err != nil {
			sourceFile.Close()
			return err
		}
		if err := sourceFile.Close(); err != nil {
			return err
		}
		return nil
	})
}
