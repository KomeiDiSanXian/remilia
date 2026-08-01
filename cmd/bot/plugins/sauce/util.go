package sauce

import (
	"errors"
	"net/http"
	"net/url"
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

// redactTransportError 抹掉传输错误 URL 中的认证查询参数。
//
// net/http 的传输错误是 *url.Error，其 Error() 会带上完整请求 URL，
// 而 URL 查询参数里可能携带 api_key 等凭据（SauceNAO / Gelbooru 等），
// 一次超时/DNS 抖动就会把凭据写进日志或回复给用户。
// 保留 errors.Is/As 判定能力（重新构造 *url.Error）。
func redactTransportError(err error) error {
	var uerr *url.Error
	if !errors.As(err, &uerr) {
		return err
	}
	parsed, perr := url.Parse(uerr.URL)
	if perr != nil {
		return err
	}
	q := parsed.Query()
	changed := false
	for _, key := range []string{"api_key", "user_id"} {
		if q.Has(key) {
			q.Set(key, "<redacted>")
			changed = true
		}
	}
	if !changed {
		return err
	}
	parsed.RawQuery = q.Encode()
	return &url.Error{Op: uerr.Op, URL: parsed.String(), Err: uerr.Err}
}
