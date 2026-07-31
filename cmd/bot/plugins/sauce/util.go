package sauce

import (
	"net/http"
	"strconv"
	"strings"
)

// ── 工具函数 ──────────────────────────────────────────────────────────

// userAgent 模拟浏览器 UA，降低被目标站点拦截的概率。
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// setBrowserHeaders 设置浏览器风格的请求头。
func setBrowserHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Origin", "https://ascii2d.net")
	req.Header.Set("Referer", referer)
}

// parseSimilarity 解析相似度字符串为浮点数，失败时返回 0。
func parseSimilarity(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil {
		return 0
	}
	return v
}
