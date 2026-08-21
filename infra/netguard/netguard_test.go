package netguard

import (
	"net"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"0.0.0.0", false},     // unspecified
		{"127.0.0.1", false},   // loopback
		{"192.168.1.1", false}, // private
		{"10.0.0.1", false},    // private
		{"172.16.0.1", false},  // private
		{"169.254.1.1", false}, // link-local (云元数据)
		{"224.0.0.1", false},   // multicast
		{"::1", false},         // loopback v6
		{"fe80::1", false},     // link-local v6
	}
	for _, c := range cases {
		if got := IsPublicIP(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("IsPublicIP(%q)=%v, want %v", c.ip, got, c.want)
		}
	}
	if IsPublicIP(nil) {
		t.Error("nil IP should not be public")
	}
}

func TestAllowURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"not-a-url", false},
		{"http://example.com/x.png", false},        // 非 https
		{"https://127.0.0.1/x.png", false},         // 环回
		{"https://192.168.1.1/x.png", false},       // 私网
		{"https://user:pass@example.com/x", false}, // 带用户信息
		{"https:///no-host", false},
		{"https://8.8.8.8/x.png", true}, // 公网 IP
	}
	for _, c := range cases {
		if got := AllowURL(c.url); got != c.want {
			t.Errorf("AllowURL(%q)=%v, want %v", c.url, got, c.want)
		}
	}
}
