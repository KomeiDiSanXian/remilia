// client.go — 出站客户端的策略装配（策略实现见 infra/outbound）。
//
// 两条出站路径的信任等级不同，各持一个客户端，互不继承名单：
//
//   - 抓取路径（fetch）：目标是用户/模型给的任意 URL → Untrusted，
//     仅 https + 公网 + 逐跳重定向校验；
//   - 搜索路径（search）：目标是**运维配置**的搜索引擎端点（自建 SearXNG
//     常在回环/内网，如 http://10.0.0.19:8080）→ OperatorEndpoint，
//     端点自身放行内网与明文、同源跳转放行；跨源目标仍按 Untrusted 处理，
//     避免实例被攻陷后借重定向把请求导向内网其它地址。
//
// 策略在构造期固化在各自客户端上，不在请求期改共享字段，因此并行工具调用
// （tool_parallel）下无数据竞争。
package websearch

import (
	"context"
	"net/http"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/outbound"
)

// maxRedirects 单次请求允许的最大重定向跳数。
const maxRedirects = 5

// doer 是出站客户端的最小接口：*outbound.Client 与 *http.Client 都满足，
// 便于搜索引擎在测试里注入 httptest 客户端。
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// newWebClient 构造网页抓取客户端：目标不可信，严格公网校验。
func newWebClient(timeout time.Duration, proxy string) (*outbound.Client, error) {
	return outbound.New(outbound.Policy{
		Component:    "websearch.fetch",
		Class:        outbound.Untrusted,
		Proxy:        proxy,
		Timeout:      timeout,
		MaxRedirects: maxRedirects,
	})
}

// newSearchClient 构造搜索引擎客户端：endpoint 为运维配置的端点，
// 作为受信源声明；endpoint 为空时无法确定信任边界，直接报错。
func newSearchClient(timeout time.Duration, proxy, endpoint string) (*outbound.Client, error) {
	return outbound.New(outbound.Policy{
		Component:    "websearch.search",
		Class:        outbound.OperatorEndpoint,
		Origins:      []string{endpoint},
		Proxy:        proxy,
		Timeout:      timeout,
		MaxRedirects: maxRedirects,
	})
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
