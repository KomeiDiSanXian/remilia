package sauce

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/netguard"
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

// newSauceDownloadClient 构建带 SSRF 防护的图片下载客户端。
//
// 下载的 URL 来自平台附件/引用消息（用户可控），须限制目标为公网地址：
// 在共享 Transport 基础上叠加 netguard.DialContext（连接前校验目标 IP）
// 与逐跳重定向校验。引擎 API 调用仍走共享 Transport（目标固定为引擎域名）。
func newSauceDownloadClient(timeout time.Duration) *http.Client {
	tr := sauceTransport.Clone()
	tr.DialContext = netguard.DialContext
	return &http.Client{
		Timeout:       timeout,
		Transport:     tr,
		CheckRedirect: netguard.RedirectPolicy(10),
	}
}
