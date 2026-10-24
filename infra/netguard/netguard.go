// Package netguard 提供出站 HTTP 下载的 SSRF 防护原语：
// URL 合法性校验（仅 https + 公网目标 IP）、受信主机名单与安全 DialContext。
//
// 适用场景：机器人插件下载用户可控 URL（平台附件直链、被引用消息附件等）。
// 防的是"诱导服务端访问内网/云元数据地址"类攻击；校验在每次连接前执行，
// 与 http.Transport 的 DialContext 双保险（防 DNS 重绑定）。
//
// 需要"URL 准入 + 拨号守卫 + 逐跳重定向"整套策略时，直接用 infra/outbound：
// 它把这三点绑定在同一个客户端里，避免每个组件各拼一套（策略分叉正是历史问题）。
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
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

// HostAllowlist 是运维显式声明的受信主机名单：命中名单的主机提供的资源不走
// "https + 公网" 准入，可用于回环/内网媒体服务（自建 OneBot 的媒体接口、
// NAS、内网图床等）。
//
// 条目形如 "host" 或 "host:port"（也接受直接粘贴 URL）：主机名大小写不敏感，
// 不带端口时匹配该主机的任意端口。名单只应来自运维配置，绝不能由用户输入
// 拼装——命中即等于对该目标开放直连。
type HostAllowlist []string

// Contains 报告 u 的主机是否命中名单（端口按 scheme 补默认值后比较）。
func (l HostAllowlist) Contains(u *url.URL) bool {
	if u == nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		port = defaultPort(u.Scheme)
	}
	for _, entry := range l {
		entryHost, entryPort, ok := parseHostEntry(entry)
		if !ok || entryHost != host {
			continue
		}
		if entryPort == "" || entryPort == port {
			return true
		}
	}
	return false
}

// ValidHostEntry 报告 entry 是否为合法的受信条目（"host"、"host:port" 或
// 完整 URL），供配置校验在启动期提前拒绝写错的名单项。
func ValidHostEntry(entry string) bool {
	host, _, ok := parseHostEntry(entry)
	return ok && host != ""
}

// ContainsAddr 报告 "host:port" 形式的拨号地址是否命中名单（拨号层使用，
// 与 [HostAllowlist.Contains] 的 URL 语义一致）。
func (l HostAllowlist) ContainsAddr(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" {
		return false
	}
	return l.Contains(&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)})
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

// parseHostEntry 解析受信条目，返回主机与端口（端口为空表示不限端口）。
//
// 接受 "host"、"host:port"、"[v6]:port"、"v6" 以及直接粘贴的 http(s) URL；
// URL 形式按 scheme 补默认端口。
func parseHostEntry(entry string) (host, port string, ok bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", "", false
	}
	if h, p, err := net.SplitHostPort(entry); err == nil && isPortNumber(p) {
		if h == "" {
			return "", "", false
		}
		return strings.ToLower(h), p, true
	}
	if u, err := url.Parse(entry); err == nil && u.Scheme != "" && u.Hostname() != "" {
		port := u.Port()
		if port == "" {
			port = defaultPort(u.Scheme)
		}
		return strings.ToLower(u.Hostname()), port, true
	}
	// 裸主机名 / IPv6 字面量：不限端口。
	return strings.ToLower(strings.Trim(entry, "[]")), "", true
}

// isPortNumber 报告 s 是否为纯数字端口。SplitHostPort 不校验端口内容，
// "http://host" 会被它误拆成 host="http" + port="//host"，故须自行校验。
func isPortNumber(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// DialAddr 把 URL 归一化为拨号地址 "host:port"，供
// （无端口时按 scheme 补 80/443）。
//
// 只接受带 http/https scheme 且含主机名的绝对 URL；其余返回 false。
func DialAddr(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = defaultPort(u.Scheme)
	}
	return net.JoinHostPort(u.Hostname(), port), true
}

// defaultPort 返回 URL 未显式携带端口时的默认端口。
func defaultPort(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

// SameOrigin 判断两个 URL 是否同源：scheme、主机名（大小写不敏感）与有效端口
// 一致，端口未显式给出时按 scheme 补 80/443。任一为 nil 或缺少主机名时返回
// false。
//
// 用途：把"运维配置端点的自我跳转/自我引用"与"跳到别处的目标"区分开——
// 前者不是 SSRF 场景，后者仍须公网校验。
func SameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || a.Hostname() == "" || b.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(a.Scheme, b.Scheme) {
		return false
	}
	return strings.EqualFold(originHostPort(a), originHostPort(b))
}

// originHostPort 返回 "host:port"（无端口时按 scheme 补默认值）。
func originHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = defaultPort(u.Scheme)
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// IsPublicIP 判断 IP 是否为公网可达地址。
//
// 排除未指定、环回、私网、链路本地（含 AWS 元数据 169.254.x）、组播地址。
func IsPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}
