package sauce

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ── 输入 / 下载 ────────────────────────────────────────────────────────

// findImageURL 从平台事件中提取第一张图片附件的 URL；无图片或 URL 为空时
// 返回空字符串。平台无法提供 URL（仅本地文件）的图片暂不支持。
func findImageURL(event platform.Event) string {
	for _, att := range event.Attachments() {
		if att.URL == "" {
			continue
		}
		if att.MimeType != "" && !strings.HasPrefix(att.MimeType, "image/") {
			continue
		}
		return att.URL
	}
	return ""
}

// downloadImage 下载远程图片字节，maxBytes 为体积上限（<=0 表示不限制）。
func downloadImage(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("图片过大 (%d bytes)", resp.ContentLength)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// detectMimeType 根据 URL 后缀与文件魔数嗅探图片 MIME 类型，供缩略图直传使用。
func detectMimeType(url string, data []byte) string {
	lower := strings.ToLower(url)
	switch {
	case strings.Contains(lower, ".png"):
		return "image/png"
	case strings.Contains(lower, ".jpg"), strings.Contains(lower, ".jpeg"):
		return "image/jpeg"
	case strings.Contains(lower, ".gif"):
		return "image/gif"
	case strings.Contains(lower, ".webp"):
		return "image/webp"
	}
	if len(data) >= 8 {
		if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
			return "image/png"
		}
		if data[0] == 0xFF && data[1] == 0xD8 {
			return "image/jpeg"
		}
		if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
			return "image/gif"
		}
		if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
			return "image/webp"
		}
	}
	return "image/jpeg"
}

// extByMime 根据 MIME 类型返回图片文件扩展名。
func extByMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}
