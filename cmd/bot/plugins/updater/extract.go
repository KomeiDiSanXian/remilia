package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxExtractedBytes 解压单个条目的软上限（二进制 ~50MB，1GB 足够宽松）。
const maxExtractedBytes = 1 << 30

// extractBinary 从 goreleaser 归档（tar.gz / zip）中解压出二进制文件，
// 返回解压后的完整路径。归档内二进制名固定为 remilia（Windows 为 remilia.exe）。
//
// 安全性：拒绝路径穿越（..），跳过目录与符号链接，限制单条解压大小。
func extractBinary(archivePath, destDir, wantName string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("创建解压目录失败: %w", err)
	}
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"), strings.HasSuffix(archivePath, ".tgz"):
		return extractTarGz(archivePath, destDir, wantName)
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZip(archivePath, destDir, wantName)
	default:
		return "", fmt.Errorf("不支持的归档格式: %s", archivePath)
	}
}

// binaryMatches 判断归档条目名是否为期望的二进制名（兼容 remilia / remilia.exe）。
func binaryMatches(name, wantName string) bool {
	base := filepath.Base(name)
	return base == wantName || base == "remilia" || base == "remilia.exe"
}

// extractEntry 将 src 解压到 destPath，限制大小并跳过符号链接。
func extractEntry(src io.Reader, destPath string, size int64) error {
	if size > maxExtractedBytes {
		return fmt.Errorf("归档条目过大（%d 字节）", size)
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(src, maxExtractedBytes+1))
	cerr := f.Close()
	if err != nil {
		os.Remove(destPath)
		return err
	}
	if cerr != nil {
		os.Remove(destPath)
		return cerr
	}
	if n > maxExtractedBytes {
		os.Remove(destPath)
		return fmt.Errorf("归档条目过大")
	}
	return nil
}

// safeJoin 确保 name 不会逃逸出 baseDir。
func safeJoin(baseDir, name string) (string, error) {
	clean := filepath.Clean(filepath.Join(baseDir, name))
	if clean != baseDir && !strings.HasPrefix(clean, baseDir+string(filepath.Separator)) {
		return "", fmt.Errorf("归档条目路径越界: %s", name)
	}
	return clean, nil
}

// extractTarGz 解压 tar.gz 归档中的二进制。
func extractTarGz(archivePath, destDir, wantName string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("打开归档失败: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("读取 gzip 失败: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("读取 tar 失败: %w", err)
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			continue
		}
		if !hdr.FileInfo().Mode().IsRegular() {
			continue
		}
		if !binaryMatches(hdr.Name, wantName) {
			continue
		}
		destPath, err := safeJoin(destDir, wantName)
		if err != nil {
			return "", err
		}
		if err := extractEntry(tr, destPath, hdr.Size); err != nil {
			return "", fmt.Errorf("解压二进制失败: %w", err)
		}
		return destPath, nil
	}
	return "", fmt.Errorf("归档中未找到二进制文件 %q", wantName)
}

// extractZip 解压 zip 归档中的二进制。
func extractZip(archivePath, destDir, wantName string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("打开 zip 归档失败: %w", err)
	}
	defer zr.Close()

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() || zf.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !binaryMatches(zf.Name, wantName) {
			continue
		}
		destPath, err := safeJoin(destDir, wantName)
		if err != nil {
			return "", err
		}
		rc, err := zf.Open()
		if err != nil {
			return "", fmt.Errorf("读取 zip 条目失败: %w", err)
		}
		err = extractEntry(rc, destPath, int64(zf.UncompressedSize64))
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("解压二进制失败: %w", err)
		}
		return destPath, nil
	}
	return "", fmt.Errorf("归档中未找到二进制文件 %q", wantName)
}
