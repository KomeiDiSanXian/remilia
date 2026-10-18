// client.go — 共享 HTTP 客户端：代理、SSRF 守卫与重定向策略。
//
// 搜索与网页抓取共用同一连接池与代理，且共用同一条安全边界：
//   - 拨号层经 [netguard.GuardTransport] 二次校验目标 IP（仅公网），
//     缩小"校验后 DNS 重绑定"窗口——URL 级校验挡不住这一步；
//   - 重定向逐跳经 [netguard.RedirectPolicy] 校验（scheme 必须仍是 https、
//     目标必须是公网地址），避免"https 入口 → http 或内网跳转"绕过入口校验。
//
// 两个策略都固化在 Transport/Client 构造期，不在请求期写共享字段，
// 并行工具调用（tool_parallel）下无数据竞争。
package websearch

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/netguard"
)

// maxRedirects 单次请求允许的最大重定向跳数。
const maxRedirects = 5

// initWebTransport 初始化共享 Transport（Setup 时调用一次）。
//
// proxyURL 为空时沿用环境变量代理或直连（与 pic/sauce 插件 proxy 语义一致）。
// 返回的 Transport 已挂上 SSRF 拨号守卫：已知代理地址照常放行，
// 直连目标一律要求公网 IP。
func initWebTransport(proxyURL string) error {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return fmt.Errorf("无效的代理地址 %q: %w", proxyURL, err)
		}
		tr.Proxy = http.ProxyURL(proxy)
	}
	webTransport = guardDial(tr)
	return nil
}

// guardDial 给 Transport 挂上 SSRF 拨号守卫（默认 [netguard.GuardTransport]）。
//
// 与同文件的 [ssrfPolicy] 一样是可替换点：httptest 服务器绑定回环地址，
// 测试需要放行本地目标时替换为直连版本；生产路径保持严格公网校验。
var guardDial = func(tr *http.Transport) *http.Transport { return netguard.GuardTransport(tr) }

// webTransport 是共享的 HTTP Transport（代理配置在 Setup 时固化）。
// 搜索与网页抓取共用同一连接池、代理与拨号守卫。
var webTransport *http.Transport

// newWebClient 基于共享 Transport 构建带指定超时的 HTTP 客户端。
//
// 未初始化（如测试直用）时回退 DefaultTransport 的守卫副本，
// 保证任何调用路径都不会拿到没有 SSRF 守卫的客户端。
func newWebClient(timeout time.Duration) *http.Client {
	tr := webTransport
	if tr == nil {
		tr = guardDial(http.DefaultTransport.(*http.Transport))
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     tr,
		CheckRedirect: netguard.RedirectPolicy(maxRedirects),
	}
}

// newRequest 构建带 UA 与超时 context 的 GET 请求。
func newRequest(ctx context.Context, u string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RemiliaBot/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	return req, nil
}
