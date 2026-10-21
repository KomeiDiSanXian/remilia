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

// DialAddrs 返回可直接传给 [GuardTransport] 的受信条目（去掉空白项）。
//
// Deprecated: 出站客户端请用 infra/outbound，它把 URL 准入、拨号与重定向
// 策略绑定在一起；单独拼装 Transport 是此前"每个组件一套策略"的成因。
func (l HostAllowlist) DialAddrs() []string {
	if len(l) == 0 {
		return nil
	}
	out := make([]string, 0, len(l))
	for _, entry := range l {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// AllowURLOrHosts 判断资源 URL 是否允许访问：默认与 [AllowURL] 一致
// （https + 公网 + 无用户信息），命中受信主机名单的额外允许 http/https 与
// 内网地址。
//
// 用于"平台/运维侧提供、但地址可能落在内网"的资源（平台附件、内网媒体服务）。
// 用户或模型直接给出的 URL 不要走这个入口。
//
// Deprecated: 用 infra/outbound 的 TrustedOrigin 类（声明来源 + 逐跳重定向
// 校验一体）。只判 URL 不下载的场景用 outbound.Allows。
func AllowURLOrHosts(rawURL string, hosts HostAllowlist) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" || u.User != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	if hosts.Contains(u) {
		return true
	}
	return strings.EqualFold(u.Scheme, "https") && IsPublicHost(u.Hostname())
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
//   - 拨号目标是 trusted 中的地址（运维配置的受信端点或受信主机，如自建
//     SearXNG 实例所在的 "10.0.0.19:8080"，由 [DialAddr] 从 URL 归一化得到）
//     时放行；条目写 "host" 表示该主机不限端口，写 "host:port" 表示精确匹配；
//   - 其余拨号（直连目标）沿用公网 IP 校验，保留 DNS 重绑定防线。
//
// trusted 只应用于**运维配置的端点**，绝不能放进用户可控 URL 的路径：
// 命中白名单即跳过公网校验，等于对该目标开放直连。
// 无代理且无受信地址时行为与直接设置 DialContext 完全一致（严格公网）。
// URL 级校验（AllowURL / RedirectPolicy）仍须由调用方叠加，本函数只处理拨号层。
//
// Deprecated: 用 infra/outbound 构造客户端。本函数只包拨号层，调用方还须自己
// 叠加 URL 准入与重定向策略——这正是策略分叉的来源。
func GuardTransport(tr *http.Transport, trusted ...string) *http.Transport {
	if tr == nil {
		return nil
	}
	cloned := tr.Clone()
	allow := proxyDialAllowlist(tr)
	for _, entry := range trusted {
		allow.add(entry)
	}
	origDial := cloned.DialContext
	if origDial == nil {
		origDial = (&net.Dialer{}).DialContext
	}
	cloned.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if allow.contains(address) {
			return origDial(ctx, network, address)
		}
		return DialContext(ctx, network, address)
	}
	return cloned
}

// dialAllowlist 拨号受信名单：exact 精确匹配 "host:port"，hosts 匹配单个主机
// 的任意端口（条目只写主机名时）。
type dialAllowlist struct {
	exact map[string]struct{}
	hosts map[string]struct{}
}

func newDialAllowlist() *dialAllowlist {
	return &dialAllowlist{exact: make(map[string]struct{}), hosts: make(map[string]struct{})}
}

// add 加入一条受信条目；格式非法时忽略（静默忽略比让启动失败更安全：
// 配置写错只会回到严格公网校验）。
func (a *dialAllowlist) add(entry string) {
	if a == nil {
		return
	}
	host, port, ok := parseHostEntry(entry)
	if !ok {
		return
	}
	if port == "" {
		a.hosts[host] = struct{}{}
		return
	}
	a.exact[net.JoinHostPort(host, port)] = struct{}{}
}

// contains 判断拨号地址是否命中受信名单（主机名大小写不敏感）。
func (a *dialAllowlist) contains(address string) bool {
	if a == nil {
		return false
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || host == "" || port == "" {
		return false
	}
	host = strings.ToLower(host)
	if _, ok := a.exact[net.JoinHostPort(host, port)]; ok {
		return true
	}
	_, ok := a.hosts[host]
	return ok
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

// proxyDialAllowlist 收集 Transport 可能拨号到的代理地址（显式代理配置 +
// 环境变量代理），归一化为 "host:port"（IPv6 加括号、无端口补 scheme 默认值）。
func proxyDialAllowlist(tr *http.Transport) *dialAllowlist {
	allow := newDialAllowlist()
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
			port = defaultPort(u.Scheme)
		}
		allow.exact[net.JoinHostPort(strings.ToLower(host), port)] = struct{}{}
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

// DialAddr 把 URL 归一化为拨号地址 "host:port"，供
// [GuardTransport] 的 trusted 参数使用（无端口时按 scheme 补 80/443）。
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

// RedirectPolicy 返回限制重定向目标的 CheckRedirect 策略。
//
// maxRedirects 为最大跳数；重定向目标需通过 AllowURL，否则拒绝。
//
// Deprecated: 用 infra/outbound 的 Policy.MaxRedirects（逐跳准入与跳数在
// 同一个客户端里完成）。
func RedirectPolicy(maxRedirects int) func(req *http.Request, via []*http.Request) error {
	return RedirectPolicyWithHosts(maxRedirects, nil)
}

// RedirectPolicyWithHosts 同 [RedirectPolicy]，但命中受信主机名单的重定向目标
// 放行（内网媒体服务常把 http 跳 https 或跳到自身的具体文件地址）。
//
// Deprecated: 用 infra/outbound 的 TrustedOrigin 类；重定向目标会自动重新
// 过一遍同一套准入。
func RedirectPolicyWithHosts(maxRedirects int, hosts HostAllowlist) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if !AllowURLOrHosts(req.URL.String(), hosts) {
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
