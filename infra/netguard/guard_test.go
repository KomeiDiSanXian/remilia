package netguard

import (
	"net/url"
	"testing"
)

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
}
