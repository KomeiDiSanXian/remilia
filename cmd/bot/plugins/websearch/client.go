package websearch

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// webTransport 是共享的 HTTP Transport（代理配置在 Setup 时固化）。
// 搜索与网页抓取共用同一连接池与代理。
var webTransport *http.Transport

// initWebTransport 初始化共享 Transport（Setup 时调用一次）。
// proxyURL 为空时沿用环境变量代理或直连（与 pic/sauce 插件 proxy 语义一致）。
func initWebTransport(proxyURL string) error {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		proxy, perr := url.Parse(proxyURL)
		if perr != nil {
			return fmt.Errorf("无效的代理地址 %q: %w", proxyURL, perr)
		}
		tr.Proxy = http.ProxyURL(proxy)
	}
	webTransport = tr
	return nil
}

// newWebClient 基于共享 Transport 构建带指定超时的 HTTP 客户端。
// webTransport 未初始化（如测试直用）时回退 DefaultTransport。
func newWebClient(timeout time.Duration) *http.Client {
	c := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			return nil
		},
	}
	if webTransport != nil {
		c.Transport = webTransport
	}
	return c
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
