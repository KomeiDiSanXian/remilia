package sauce

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/outbound"
)

// ── 共享 HTTP 传输层 ───────────────────────────────────────────────────
//
// 所有引擎（SauceNAO / IQDB / TraceMoe / AnimeTrace）共用同一套代理配置：
// plugins.sauce.proxy 为空时沿用环境变量代理或直连，与 pic 插件的 proxy
// 语义一致。
//
// 共享的是 Transport（连接池 + 代理），各引擎客户端持有独立超时的
// http.Client——IQDB 高峰期排队需要更长超时，不能与其它引擎共用。
//
// 图片下载不走这条共享 Transport：它面对的是平台附件/引擎返回的 URL，
// 需要独立的信任策略（见 newSauceDownloadClient）。

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

// newSauceDownloadClient 构建图片下载客户端（策略见 infra/outbound）。
//
// 下载的 URL 来自平台附件、引用消息与引擎返回的缩略图，属半可信输入：
// 默认按"https + 公网"准入，逐跳校验重定向，拨号前对解析结果二次校验。
// origins 是运维显式声明的媒体来源（allow_private_media_hosts 与平台适配器
// 的自声明来源）：命中来源的主机允许内网与明文，其余目标不变。
func newSauceDownloadClient(timeout time.Duration, proxy string, origins []string) (*outbound.Client, error) {
	return outbound.New(outbound.Policy{
		Component:    "sauce.download",
		Class:        outbound.TrustedOrigin,
		Origins:      origins,
		Proxy:        proxy,
		Timeout:      timeout,
		MaxRedirects: 10,
	})
}
