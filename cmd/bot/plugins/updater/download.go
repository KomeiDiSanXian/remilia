package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// maxAssetBytes 下载归档的软上限（当前二进制 ~50MB，512MB 足够宽松）。
const maxAssetBytes = 512 << 20

// requireHTTPS 是否强制 HTTPS 下载（生产恒为 true；测试可关闭以使用 httptest）。
var requireHTTPS = true

// downloadFile 将 url 下载到 destPath（临时文件 → 原子改名），返回字节数。
//
// 安全性：仅接受 https；跟随重定向（GitHub 资产会 302 到 objects.githubusercontent.com）；
// 错误信息中的 URL 会剥离查询串，避免泄露任何可能嵌入的凭据。
func downloadFile(ctx context.Context, hc *http.Client, url, destPath string) (int64, error) {
	if requireHTTPS && !strings.HasPrefix(url, "https://") {
		return 0, fmt.Errorf("拒绝非 HTTPS 下载地址")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "remilia-updater")

	resp, err := hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("下载失败 (%s): %w", redactURL(url), err)
	}
	defer resp.Body.Close()

	// 重定向后的最终 URL 必须仍是 HTTPS（防止 302 逃逸到明文通道）
	if requireHTTPS && (resp.Request == nil || resp.Request.URL == nil ||
		!strings.EqualFold(resp.Request.URL.Scheme, "https")) {
		return 0, fmt.Errorf("下载被重定向到非 HTTPS 地址，已拒绝")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("下载失败 (%s): HTTP %d", redactURL(url), resp.StatusCode)
	}
	if resp.ContentLength > maxAssetBytes {
		return 0, fmt.Errorf("下载内容过大（%d 字节，上限 %d）", resp.ContentLength, maxAssetBytes)
	}

	tmp := destPath + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer func() {
		f.Close()
		os.Remove(tmp)
	}()

	n, err := io.Copy(f, io.LimitReader(resp.Body, maxAssetBytes+1))
	if err != nil {
		return 0, fmt.Errorf("写入下载内容失败: %w", err)
	}
	if n > maxAssetBytes {
		return 0, fmt.Errorf("下载内容过大（上限 %d 字节）", maxAssetBytes)
	}
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("落盘失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("关闭文件失败: %w", err)
	}
	if err := os.Rename(tmp, destPath); err != nil {
		return 0, fmt.Errorf("保存文件失败: %w", err)
	}
	return n, nil
}

// redactURL 剥离 URL 的查询串（下载地址可能携带临时签名参数）。
func redactURL(u string) string {
	if before, _, ok := strings.Cut(u, "?"); ok {
		return before
	}
	return u
}
