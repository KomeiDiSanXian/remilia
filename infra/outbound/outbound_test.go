package outbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/netguard"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPolicyValidate(t *testing.T) {
	tests := []struct {
		name    string
		policy  Policy
		wantErr bool
	}{
		{
			name:   "untrusted 无源合法",
			policy: Policy{Component: "t", Class: Untrusted},
		},
		{
			name:    "untrusted 不允许声明源",
			policy:  Policy{Component: "t", Class: Untrusted, Origins: []string{"nas.lan"}},
			wantErr: true,
		},
		{
			name:    "operator 端点必须有源",
			policy:  Policy{Component: "t", Class: OperatorEndpoint},
			wantErr: true,
		},
		{
			name:   "operator 端点带 URL 源",
			policy: Policy{Component: "t", Class: OperatorEndpoint, Origins: []string{"http://10.0.0.19:8080"}},
		},
		{
			name:   "host 条目合法",
			policy: Policy{Component: "t", Class: TrustedOrigin, Origins: []string{"nas.lan"}},
		},
		{
			name:   "host:port 条目合法",
			policy: Policy{Component: "t", Class: TrustedOrigin, Origins: []string{"nas.lan:5000"}},
		},
		{
			name:    "空条目非法",
			policy:  Policy{Component: "t", Class: TrustedOrigin, Origins: []string{"  "}},
			wantErr: true,
		},
		{
			name:    "SameOriginOnly 需要 URL 源",
			policy:  Policy{Component: "t", Class: OperatorEndpoint, Origins: []string{"10.0.0.1:3000"}, SameOriginOnly: true},
			wantErr: true,
		},
		{
			name:    "代理地址非法",
			policy:  Policy{Component: "t", Proxy: "://bad"},
			wantErr: true,
		},
		{
			name:    "未知类非法",
			policy:  Policy{Component: "t", Class: Class(99)},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("期望校验失败，实际通过")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望校验通过，实际失败: %v", err)
			}
		})
	}
}

// TestAllowsAdmissionOnly 回归：只校验不下载的入口（模型给出的图片地址）与
// 下载路径用同一套判定，但不建客户端、不发请求。
func TestAllowsAdmissionOnly(t *testing.T) {
	untrusted := Policy{Component: "test.allows", Class: Untrusted}
	if err := Allows("https://93.184.216.34/a.png", untrusted); err != nil {
		t.Fatalf("公网 https 应放行: %v", err)
	}
	if reason, ok := Blocked(Allows("http://93.184.216.34/a.png", untrusted)); !ok || reason != ReasonPlainHTTP {
		t.Fatalf("明文 http 原因 = %v/%v，期望 %s", reason, ok, ReasonPlainHTTP)
	}
	if reason, ok := Blocked(Allows("https://127.0.0.1/a.png", untrusted)); !ok || reason != ReasonPrivateAddress {
		t.Fatalf("回环原因 = %v/%v，期望 %s", reason, ok, ReasonPrivateAddress)
	}
	// 受信来源同样生效（平台/运维声明后同一入口放行）。
	trusted := Policy{
		Component: "test.allows.trusted",
		Class:     TrustedOrigin,
		Origins:   []string{"127.0.0.1:8080"},
	}
	if err := Allows("http://127.0.0.1:8080/a.png", trusted); err != nil {
		t.Fatalf("受信来源应放行: %v", err)
	}
}

func TestAllowMatrix(t *testing.T) {
	strict, err := New(Policy{Component: "test.strict", Class: Untrusted})
	if err != nil {
		t.Fatalf("构造严格客户端失败: %v", err)
	}
	trusted, err := New(Policy{
		Component: "test.trusted",
		Class:     TrustedOrigin,
		Origins:   []string{"127.0.0.1:8080", "media.lan"},
	})
	if err != nil {
		t.Fatalf("构造受信客户端失败: %v", err)
	}

	tests := []struct {
		name   string
		client *Client
		url    string
		reason Reason // 空表示允许
	}{
		{"严格-公网 https", strict, "https://93.184.216.34/a.png", ""},
		{"严格-明文 http", strict, "http://93.184.216.34/a.png", ReasonPlainHTTP},
		{"严格-回环", strict, "https://127.0.0.1/a.png", ReasonPrivateAddress},
		{"严格-私网", strict, "https://10.1.2.3/a.png", ReasonPrivateAddress},
		{"严格-云元数据", strict, "https://169.254.169.254/latest/meta-data", ReasonPrivateAddress},
		{"严格-畸形", strict, "not a url", ReasonMalformed},
		{"严格-用户信息", strict, "https://user:pass@93.184.216.34/a.png", ReasonUserInfo},
		{"严格-非 http 协议", strict, "file:///etc/passwd", ReasonScheme},
		{"受信-精确端口", trusted, "http://127.0.0.1:8080/file.png", ""},
		{"受信-主机不限端口", trusted, "http://media.lan:9000/file.png", ""},
		{"受信-端口不符且明文", trusted, "http://127.0.0.1:9090/file.png", ReasonPlainHTTP},
		{"受信-端口不符 https 私网", trusted, "https://127.0.0.1:9090/file.png", ReasonPrivateAddress},
		{"受信-跨源公网 https 仍允许", trusted, "https://93.184.216.34/file.png", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.client.Allow(tc.url)
			if tc.reason == "" {
				if err != nil {
					t.Fatalf("期望允许，实际被拦: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望被拦（%s），实际允许", tc.reason)
			}
			got, ok := Blocked(err)
			if !ok {
				t.Fatalf("期望 *BlockError，实际 %T: %v", err, err)
			}
			if got != tc.reason {
				t.Fatalf("拦截原因 = %s，期望 %s", got, tc.reason)
			}
		})
	}
}

// TestBlockedMetricDelta 断言拦截指标按 (component, reason) 递增；只比较
// 调用前后的差值，避免依赖包级指标的绝对值。
func TestBlockedMetricDelta(t *testing.T) {
	client, err := New(Policy{Component: "test.metric", Class: Untrusted})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	before := testutil.ToFloat64(blockedTotal.WithLabelValues("test.metric", string(ReasonPrivateAddress)))
	_ = client.Allow("https://127.0.0.1/x")
	after := testutil.ToFloat64(blockedTotal.WithLabelValues("test.metric", string(ReasonPrivateAddress)))
	if after-before != 1 {
		t.Fatalf("拦截指标增量 = %v，期望 1", after-before)
	}
}

// TestGetTrustedOrigin 受信源可以直接访问回环上的服务，且请求确实发出去了。
func TestGetTrustedOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client, err := New(Policy{Component: "test.get", Class: TrustedOrigin, Origins: []string{srv.URL}})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.Get(ctx, srv.URL+"/")
	if err != nil {
		t.Fatalf("受信源请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", resp.StatusCode)
	}
}

// TestUntrustedBlocksBeforeDial 严格客户端在发出请求前就拒绝回环目标，
// 不会产生任何网络连接。
func TestUntrustedBlocksBeforeDial(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
	}))
	defer srv.Close()

	client, err := New(Policy{Component: "test.strict", Class: Untrusted})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	if _, err := client.Get(context.Background(), srv.URL+"/"); err == nil {
		t.Fatal("期望被拦，实际成功")
	}
	if hits != 0 {
		t.Fatalf("被拦的请求不应到达服务端，实际命中 %d 次", hits)
	}
}

// TestRedirectPolicy 校验逐跳策略：跨源明文被拦、同源放行、跨源但受信放行。
func TestRedirectPolicy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("target"))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/landing", http.StatusFound)
		case "/cross":
			http.Redirect(w, r, target.URL+"/final", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer source.Close()

	onlySource, err := New(Policy{
		Component: "test.redirect.src",
		Class:     TrustedOrigin,
		Origins:   []string{source.URL},
	})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 同源跳转放行。
	resp, err := onlySource.Get(ctx, source.URL+"/same")
	if err != nil {
		t.Fatalf("同源跳转应放行，实际失败: %v", err)
	}
	resp.Body.Close()

	// 跨源到未声明的主机：明文 + 回环，被拦。
	if _, err := onlySource.Get(ctx, source.URL+"/cross"); err == nil {
		t.Fatal("跨源跳转应被拦，实际成功")
	} else if reason, ok := Blocked(err); !ok || reason != ReasonPlainHTTP {
		t.Fatalf("跨源跳转拦截原因 = %v/%v，期望 %s", reason, ok, ReasonPlainHTTP)
	}

	// 两个源都声明后，跨源跳转放行。
	both, err := New(Policy{
		Component: "test.redirect.both",
		Class:     TrustedOrigin,
		Origins:   []string{source.URL, target.URL},
	})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	resp, err = both.Get(ctx, source.URL+"/cross")
	if err != nil {
		t.Fatalf("受信跨源跳转应放行，实际失败: %v", err)
	}
	resp.Body.Close()
}

// TestSameOriginOnly 开启同源约束后，即使目标在受信名单里，跨源跳转也被拒绝。
func TestSameOriginOnly(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	}))
	defer source.Close()

	client, err := New(Policy{
		Component:      "test.same-origin",
		Class:          OperatorEndpoint,
		Origins:        []string{source.URL, target.URL},
		SameOriginOnly: true,
	})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Get(ctx, source.URL+"/")
	if err == nil {
		t.Fatal("跨源跳转应被拒绝，实际成功")
	}
	if reason, ok := Blocked(err); !ok || reason != ReasonCrossOrigin {
		t.Fatalf("拦截原因 = %v/%v，期望 %s", reason, ok, ReasonCrossOrigin)
	}
}

// TestRedirectLimit 重定向跳数超限被拦。
func TestRedirectLimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+r.URL.Path+"x", http.StatusFound)
	}))
	defer srv.Close()

	client, err := New(Policy{
		Component:    "test.redirect-limit",
		Class:        TrustedOrigin,
		Origins:      []string{srv.URL},
		MaxRedirects: 2,
	})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Get(ctx, srv.URL+"/loop")
	if err == nil {
		t.Fatal("无限重定向应被拦，实际成功")
	}
	if reason, ok := Blocked(err); !ok || reason != ReasonRedirects {
		t.Fatalf("拦截原因 = %v/%v，期望 %s", reason, ok, ReasonRedirects)
	}
}

// TestDialBypassesProxyAddress 配置代理后，拨号目标是代理本身（通常在本机
// 回环），不得被公网校验拦下——否则所有经代理的出站请求都会失败（此前
// sauce / websearch / AI 附件都踩过这条）。
func TestDialBypassesProxyAddress(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer proxy.Close()

	client, err := New(Policy{Component: "test.proxy", Proxy: proxy.URL})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	addr := strings.TrimPrefix(proxy.URL, "http://")
	conn, err := client.dialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("拨号到代理地址应放行，实际失败: %v", err)
	}
	_ = conn.Close()
}

// TestBlockErrorIsError 保证 BlockError 能被 errors.As 识别且不泄漏组件名。
func TestBlockErrorIsError(t *testing.T) {
	client, err := New(Policy{Component: "secret.component", Class: Untrusted})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	err = client.Allow("https://127.0.0.1/x")
	var be *BlockError
	if !errors.As(err, &be) {
		t.Fatalf("期望 *BlockError，实际 %T", err)
	}
	if be.Component != "secret.component" || be.Reason != ReasonPrivateAddress {
		t.Fatalf("BlockError 字段异常: %+v", be)
	}
	if err.Error() != ReasonPrivateAddress.message() {
		t.Fatalf("Error() = %q，不应包含组件名", err.Error())
	}
}

// TestSanitizeDropsSecrets 拦截日志/指标里的 URL 不应带查询串与用户信息。
func TestSanitizeDropsSecrets(t *testing.T) {
	got := sanitize("https://user:pass@example.com/path?token=secret#frag")
	if got != "https://example.com/path" {
		t.Fatalf("sanitize = %q", got)
	}
}

// TestContainsAddr 拨号地址匹配：精确端口命中，端口不符不命中，主机条目不限端口。
func TestContainsAddr(t *testing.T) {
	hosts := netguard.HostAllowlist{"nas.lan", "127.0.0.1:8080"}
	if !hosts.ContainsAddr("127.0.0.1:8080") {
		t.Error("精确端口应命中")
	}
	if hosts.ContainsAddr("127.0.0.1:9090") {
		t.Error("端口不符不应命中")
	}
	if !hosts.ContainsAddr("nas.lan:1234") {
		t.Error("主机条目应不限端口")
	}
}
