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
