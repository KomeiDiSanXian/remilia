package sauce

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ── 共享 HTTP 传输层 ───────────────────────────────────────────────────
//
// 所有引擎（SauceNAO / IQDB / TraceMoe / AnimeTrace）与图片下载共用
// 同一套代理配置：plugins.sauce.proxy 为空时沿用环境变量代理或直连，
// 与 pic 插件的 proxy 语义一致。
//
// 共享的是 Transport（连接池 + 代理），各引擎客户端持有独立超时的
// http.Client——IQDB 高峰期排队需要更长超时，不能与其它引擎共用。

// sauceTransport 是共享的 HTTP Transport（代理配置在 Setup 时固化）。
var sauceTransport *http.Transport

// initSauceTransport 初始化共享 Transport（Setup 时调用一次）。
// proxyURL 为空时沿用环境变量代理或直连。
func initSauceTransport(proxyURL string) error {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		proxy, perr := url.Parse(proxyURL)
		if perr != nil {
			return fmt.Errorf("无效的代理地址 %q: %w", proxyURL, perr)
		}
		tr.Proxy = http.ProxyURL(proxy)
	}
	sauceTransport = tr
	return nil
}

// newSauceHTTPClient 基于共享 Transport 构建带指定超时的 HTTP 客户端。
func newSauceHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: sauceTransport,
	}
}
