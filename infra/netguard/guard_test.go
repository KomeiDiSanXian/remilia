package netguard

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startLocalListener 在回环地址上开一个监听器并返回其地址（用作代理地址）。
func startLocalListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func TestGuardTransport_Nil(t *testing.T) {
	assert.Nil(t, GuardTransport(nil))
}

// TestGuardTransport_ExplicitProxyDialAllowed 显式代理配置：拨号到代理地址
// 必须放行（此前会被公网校验误伤，报 non-public address blocked）。
func TestGuardTransport_ExplicitProxyDialAllowed(t *testing.T) {
	proxyAddr := startLocalListener(t)
	proxyURL, err := url.Parse("http://" + proxyAddr)
	require.NoError(t, err)

	guarded := GuardTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL)})
	conn, err := guarded.DialContext(context.Background(), "tcp", proxyAddr)
	require.NoError(t, err, "dial to configured proxy must be allowed")
	_ = conn.Close()
}

// TestGuardTransport_EnvProxyDialAllowed 环境变量代理：即使 tr.Proxy 为空，
// 也应识别 HTTP(S)_PROXY/ALL_PROXY 配置并放行对应代理地址的拨号。
func TestGuardTransport_EnvProxyDialAllowed(t *testing.T) {
	proxyAddr := startLocalListener(t)
	t.Setenv("HTTPS_PROXY", "http://"+proxyAddr)

	guarded := GuardTransport(&http.Transport{})
	conn, err := guarded.DialContext(context.Background(), "tcp", proxyAddr)
	require.NoError(t, err, "dial to environment proxy must be allowed")
	_ = conn.Close()
}

// TestGuardTransport_DirectTargetStillBlocked 直连场景行为不变：非公网目标仍被拦截。
func TestGuardTransport_DirectTargetStillBlocked(t *testing.T) {
	guarded := GuardTransport(&http.Transport{})
	_, err := guarded.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	require.ErrorContains(t, err, "non-public address blocked")
}

// TestGuardTransport_TrustedEndpointAllowed 运维配置的受信端点（如内网自建
// SearXNG 的 10.0.0.19:8080）必须放行：公网校验会把这类地址误伤成
// "connection to non-public address blocked"。
func TestGuardTransport_TrustedEndpointAllowed(t *testing.T) {
	addr := startLocalListener(t)
	guarded := GuardTransport(&http.Transport{}, addr)
	conn, err := guarded.DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err, "dial to a trusted endpoint must be allowed")
	_ = conn.Close()
}

// TestGuardTransport_TrustedEndpointIsExact 受信名单按 host:port 精确匹配：
// 同主机的其它端口、白名单之外的私网地址照旧拦截（白名单不得变成后门）。
func TestGuardTransport_TrustedEndpointIsExact(t *testing.T) {
	addr := startLocalListener(t)
	guarded := GuardTransport(&http.Transport{}, addr)
	_, err := guarded.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	require.ErrorContains(t, err, "non-public address blocked",
		"only the exact trusted host:port may bypass the public-IP check")
}

func TestDialAddr(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"http://10.0.0.19:8080", "10.0.0.19:8080", true},
		{"http://127.0.0.1:8080/", "127.0.0.1:8080", true},
		{"https://searx.example.com", "searx.example.com:443", true},
		{"https://searx.example.com/", "searx.example.com:443", true},
		{"http://searx.example.com", "searx.example.com:80", true},
		{"http://[::1]:8080", "[::1]:8080", true},
		{"", "", false},
		{"searx.local:8080", "", false}, // 缺 scheme
		{"http:///no-host", "", false},
	}
	for _, c := range cases {
		got, ok := DialAddr(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("DialAddr(%q) = (%q, %v), want (%q, %v)", c.raw, got, ok, c.want, c.ok)
		}
	}
}

// TestSameOrigin 同源判断：主机名大小写不敏感、端口按 scheme 补默认值，
// scheme 或有效端口不同即不同源。用于区分"配置端点自身"与"跳到别处"。
func TestSameOrigin(t *testing.T) {
	parse := func(raw string) *url.URL {
		t.Helper()
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		return u
	}
	cases := []struct {
		a, b string
		want bool
	}{
		{"http://10.0.0.19:8080", "http://10.0.0.19:8080/search", true},
		{"http://10.0.0.19:8080", "http://10.0.0.19:8080", true},
		{"https://searx.example.com", "https://searx.example.com/x", true},
		{"https://SEARX.Example.com", "https://searx.example.com/x", true},
		{"https://searx.example.com", "https://searx.example.com:443/x", true},
		{"http://searx.example.com", "http://searx.example.com:8080/x", false},
		{"http://10.0.0.19:8080", "http://10.0.0.19:9090/x", false},
		{"http://10.0.0.19:8080", "https://10.0.0.19:8080/x", false},
		{"http://10.0.0.19:8080", "http://10.0.0.20:8080/x", false},
	}
	for _, c := range cases {
		if got := SameOrigin(parse(c.a), parse(c.b)); got != c.want {
			t.Errorf("SameOrigin(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if SameOrigin(nil, parse("http://a:1")) || SameOrigin(parse("http://a:1"), nil) {
		t.Error("nil URL must not be same-origin")
	}
}

// TestGuardTransport_ProxyAllowlistDoesNotOpenHole 代理白名单不得放行
// 白名单之外的非公网地址（防止把白名单变成 SSRF 后门）。
func TestGuardTransport_ProxyAllowlistDoesNotOpenHole(t *testing.T) {
	proxyURL, err := url.Parse("http://127.0.0.1:7890")
	require.NoError(t, err)

	guarded := GuardTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL)})
	_, err = guarded.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	require.ErrorContains(t, err, "non-public address blocked",
		"non-proxy loopback address must stay blocked")
}

// TestHostAllowlist 受信主机名单：条目可写主机（不限端口）或 host:port（精确），
// 主机名大小写不敏感。
func TestHostAllowlist(t *testing.T) {
	hosts := HostAllowlist{"Media.Example.com", "10.0.0.19:8080", "http://nas.lan/"}
	parse := func(raw string) *url.URL {
		t.Helper()
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		return u
	}
	cases := []struct {
		raw  string
		want bool
	}{
		{"http://media.example.com/a.png", true},       // 任意端口
		{"https://media.example.com:8443/a.png", true}, // 同上
		{"http://10.0.0.19:8080/a.png", true},          // 端口精确命中
		{"http://10.0.0.19:9090/a.png", false},         // 端口不同
		{"http://10.0.0.19/a.png", false},              // 默认端口不匹配
		{"http://nas.lan/outputs/a.png", true},         // URL 形式按 scheme 补端口
		{"https://nas.lan/outputs/a.png", false},       // scheme 不同 -> 端口不同
		{"http://other.example.com/a.png", false},
	}
	for _, c := range cases {
		if got := hosts.Contains(parse(c.raw)); got != c.want {
			t.Errorf("Contains(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
	if HostAllowlist(nil).Contains(parse("http://media.example.com/a.png")) {
		t.Error("empty allowlist must not match")
	}
	if len(hosts.DialAddrs()) != 3 {
		t.Errorf("DialAddrs = %v, want 3 entries", hosts.DialAddrs())
	}
}

// TestAllowURLOrHosts 资源 URL 准入：默认仍是 https + 公网；命中受信名单后
// 允许 http 与内网地址，但 scheme/用户信息限制不变。
func TestAllowURLOrHosts(t *testing.T) {
	hosts := HostAllowlist{"127.0.0.1:8080"}

	if AllowURLOrHosts("http://127.0.0.1:8080/a.png", nil) {
		t.Error("http loopback must be rejected without an allowlist")
	}
	if !AllowURLOrHosts("http://127.0.0.1:8080/a.png", hosts) {
		t.Error("trusted loopback http must be allowed")
	}
	if AllowURLOrHosts("http://127.0.0.1:9090/a.png", hosts) {
		t.Error("port outside the allowlist must be rejected")
	}
	if AllowURLOrHosts("http://user:pass@127.0.0.1:8080/a.png", hosts) {
		t.Error("URL userinfo must stay rejected")
	}
	if AllowURLOrHosts("file:///etc/passwd", hosts) {
		t.Error("non-http(s) scheme must stay rejected")
	}
	if AllowURLOrHosts("https://example.com/a.png", hosts) != AllowURL("https://example.com/a.png") {
		t.Error("non-trusted targets must keep AllowURL semantics")
	}
}

// TestGuardTransport_HostOnlyEntry 只写主机名的受信条目（不限端口）同样放行拨号。
func TestGuardTransport_HostOnlyEntry(t *testing.T) {
	addr := startLocalListener(t)
	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	guarded := GuardTransport(&http.Transport{}, host)
	conn, err := guarded.DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err, "host-only allowlist entry must allow any port on that host")
	_ = conn.Close()
}

// TestRedirectPolicyWithHosts 受信主机的重定向放行，其余仍按公网校验。
func TestRedirectPolicyWithHosts(t *testing.T) {
	policy := RedirectPolicyWithHosts(10, HostAllowlist{"127.0.0.1:8080"})
	req := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return &http.Request{URL: u}
	}
	assert.NoError(t, policy(req("http://127.0.0.1:8080/b.png"), nil))
	assert.Error(t, policy(req("http://127.0.0.1:9090/b.png"), nil))
	assert.Error(t, policy(req("http://example.com/b.png"), nil))
}
