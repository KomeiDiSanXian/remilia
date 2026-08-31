// Package netguard 提供出站 HTTP 下载的 SSRF 防护助手：
// URL 合法性校验（仅 https + 公网目标 IP）与安全 DialContext / CheckRedirect。
//
// 适用场景：机器人插件下载用户可控 URL（平台附件直链、被引用消息附件等）。
// 防的是"诱导服务端访问内网/云元数据地址"类攻击；校验在每次连接前执行，
// 与 http.Transport 的 DialContext 双保险（防 DNS 重绑定）。
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// lookupTimeout 域名解析超时。
const lookupTimeout = 5 * time.Second

// AllowURL 判断远程下载 URL 是否允许访问。
//
// 只允许 https 协议、无 URL 用户信息；目标为域名时执行 DNS 解析并要求
// 全部解析结果均为公网 IP。不合法返回 false。
func AllowURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return false
	}
	return IsPublicHost(u.Hostname())
}

// IsPublicHost 判断主机名是否允许访问（SSRF 防护）。
//
// 主机名为 IP 时直接判定；为域名时执行 DNS 解析并要求全部解析结果
// 均为公网 IP。解析失败或存在任一非公网解析结果返回 false。
func IsPublicHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return IsPublicIP(ip)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !IsPublicIP(ip) {
			return false
		}
	}
	return true
}

// DialContext 仅允许连接公网地址的 dial 函数，用于 http.Transport.DialContext。
//
// 与 AllowURL 分开实现：连接建立前对最终解析到的 IP 二次校验，
// 缩小 DNS 重绑定窗口。
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return nil, fmt.Errorf("connection to non-public address blocked")
		}
	} else {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP address found for host")
		}
		for _, ip := range ips {
			if !IsPublicIP(ip) {
				return nil, fmt.Errorf("host resolves to non-public address")
			}
		}
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

// GuardTransport 返回克隆后的 Transport，拨号器替换为代理感知的 SSRF 守卫：
//
//   - 拨号目标是已配置代理（显式 tr.Proxy 或环境变量 HTTP(S)_PROXY/ALL_PROXY）
//     时放行（走原拨号器）——代理通常位于本机回环/内网，公网校验会误伤；
//   - 其余拨号（直连目标）沿用公网 IP 校验，保留 DNS 重绑定防线。
//
// 无代理时行为与直接设置 DialContext 完全一致（严格公网）。
// URL 级校验（AllowURL / RedirectPolicy）仍须由调用方叠加，本函数只处理拨号层。
func GuardTransport(tr *http.Transport) *http.Transport {
	if tr == nil {
		return nil
	}
	cloned := tr.Clone()
	allow := proxyDialAllowlist(tr)
	origDial := cloned.DialContext
	if origDial == nil {
		origDial = (&net.Dialer{}).DialContext
	}
	cloned.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if _, ok := allow[address]; ok {
			return origDial(ctx, network, address)
		}
		return DialContext(ctx, network, address)
	}
	return cloned
}

// proxyDialAllowlist 收集 Transport 可能拨号到的代理地址（显式代理配置 +
// 环境变量代理），归一化为 "host:port"（IPv6 加括号、无端口补 scheme 默认值）。
func proxyDialAllowlist(tr *http.Transport) map[string]struct{} {
	allow := make(map[string]struct{})
	add := func(u *url.URL) {
		if u == nil || u.Host == "" {
			return
		}
		host := u.Hostname()
		if host == "" {
			return
		}
		port := u.Port()
		if port == "" {
			port = defaultProxyPort(u.Scheme)
		}
		allow[net.JoinHostPort(host, port)] = struct{}{}
	}
	if tr.Proxy != nil {
		// 显式代理（ProxyURL）对所有请求返回同一地址；ProxyFromEnvironment
		// 按探测主机返回环境代理。探测主机选不常见的域名，降低被 NO_PROXY
		// 豁免导致漏收集的概率（下方环境变量扫描兜底）。
		probe := &http.Request{URL: &url.URL{Scheme: "https", Host: "netguard-probe.invalid"}}
		if pu, err := tr.Proxy(probe); err == nil {
			add(pu)
		}
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		if raw := os.Getenv(key); raw != "" {
			if u, err := url.Parse(raw); err == nil {
				add(u)
			}
		}
	}
	return allow
}

// defaultProxyPort 返回代理 URL 未显式携带端口时的默认端口。
func defaultProxyPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// RedirectPolicy 返回限制重定向目标的 CheckRedirect 策略。
//
// maxRedirects 为最大跳数；重定向目标需通过 AllowURL，否则拒绝。
func RedirectPolicy(maxRedirects int) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if !AllowURL(req.URL.String()) {
			return fmt.Errorf("redirect to unsafe URL blocked")
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}
}

// IsPublicIP 判断 IP 是否为公网可达地址。
//
// 排除未指定、环回、私网、链路本地（含 AWS 元数据 169.254.x）、组播地址。
func IsPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}
