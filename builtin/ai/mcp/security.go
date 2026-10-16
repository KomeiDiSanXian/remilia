package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// security.go — 接入外部工具服务器时的安全边界。
//
// 外部工具服务器是不可信对端：stdio 会启动本机进程，http 会向外部发起请求，
// 二者都让模型获得间接的外部执行能力。因此：
//   - 服务器是否允许启动、用什么命令、传哪些环境变量，都需显式声明；
//   - http 地址做 SSRF 防护（连接前校验解析出的 IP），始终校验 TLS；
//   - 协议时代也需显式声明：握手流程与无状态流程（allow_stateless）是两条
//     独立的对端交互路径，后者会引入 server/discover 与 subscriptions/listen，
//     交互面更宽，故默认关闭。无论走哪条流程，工具都经同一策略闸门执行——
//     这里是"能不能连"的闸门，策略闸门是 decideToolInvocation，二者不可互相代替；
//   - 服务器返回的工具注解只能作为策略推导的**输入建议**，不决定放行
//     （权威策略在 Remilia 的 ActionPolicy 侧）。

// 允许从宿主环境透传的基础变量（保证子进程能正常运行）；其余键默认不透传，
// 避免把密钥等敏感变量被动带进外部进程。
var baseEnvAllowlist = []string{
	"PATH", "HOME", "USERPROFILE", "SYSTEMROOT", "SystemRoot", "TEMP", "TMP",
	"LANG", "LC_ALL", "LC_CTYPE", "PATHEXT", "COMSPEC", "WINDIR", "PWD",
}

// validateServer 校验单个服务器配置（不检查重名，重名由管理器统一校验）。
func validateServer(cfg *Config, s ServerConfig) error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("mcp: server name is required")
	}
	switch s.Transport {
	case "stdio":
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("mcp: server %q: stdio command is required", s.Name)
		}
		if !commandAllowed(cfg.AllowedCommands, s.Command) {
			return fmt.Errorf("mcp: server %q: command %q is not in allowed_commands", s.Name, s.Command)
		}
	case "http":
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("mcp: server %q: http url is required", s.Name)
		}
		if _, err := checkHTTPURL(s.URL, s.AllowInsecureHTTP, s.AllowPrivateNetworks); err != nil {
			return fmt.Errorf("mcp: server %q: %w", s.Name, err)
		}
	default:
		return fmt.Errorf("mcp: server %q: unknown transport %q (want stdio or http)", s.Name, s.Transport)
	}
	if s.CACertFile != "" {
		if _, err := loadCACertPool(s.CACertFile); err != nil {
			return fmt.Errorf("mcp: server %q: %w", s.Name, err)
		}
	}
	return nil
}

// commandAllowed 判断命令是否在白名单内（精确匹配）。
func commandAllowed(allow []string, cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, a := range allow {
		if strings.TrimSpace(a) == cmd {
			return true
		}
	}
	return false
}

// checkHTTPURL 校验 http 传输地址：协议、明文限制与 IP 级别 SSRF 防护。
// allowInsecureHTTP 的语义是"允许明文 http，且仅限回环地址"——开启后也不
// 得连公网/局域网明文（此前只查 scheme，一个开关就把任意公网明文放行了）。
func checkHTTPURL(raw string, allowInsecure, allowPrivate bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url scheme must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("url host is empty")
	}
	if u.Scheme == "http" && !allowInsecure {
		return nil, fmt.Errorf("plain http is disabled; use https or set allow_insecure_http (loopback only)")
	}
	// 地址为字面 IP 时直接校验；主机名在连接时（拨号前）按解析结果校验。
	if ip := net.ParseIP(host); ip != nil {
		if u.Scheme == "http" && !ip.IsLoopback() {
			return nil, fmt.Errorf("plain http is restricted to loopback addresses, got %s", ip)
		}
		if err := forbidIP(ip, allowInsecure, allowPrivate); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// forbidIP 判断目标 IP 是否被禁止（SSRF 防护）。
func forbidIP(ip net.IP, allowLoopback, allowPrivate bool) error {
	if ip.IsUnspecified() {
		return fmt.Errorf("destination %s is not allowed (unspecified address)", ip)
	}
	if ip.IsLoopback() && !allowLoopback {
		return fmt.Errorf("destination %s is not allowed (loopback)", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("destination %s is not allowed (link-local)", ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("destination %s is not allowed (multicast)", ip)
	}
	if ip.IsPrivate() && !allowPrivate {
		return fmt.Errorf("destination %s is not allowed (private network)", ip)
	}
	return nil
}

// dialIPCheck 拨号前对目标 IP 的校验：SSRF 防护 + 明文 http 的回环限制。
func dialIPCheck(ip net.IP, s ServerConfig, plainHTTP bool) error {
	if err := forbidIP(ip, s.AllowInsecureHTTP, s.AllowPrivateNetworks); err != nil {
		return err
	}
	if plainHTTP && !ip.IsLoopback() {
		return fmt.Errorf("plain http is restricted to loopback addresses, got %s", ip)
	}
	return nil
}

// newHTTPClient 构造始终校验 TLS 的客户端，并在拨号前按解析出的 IP 做 SSRF
// 防护（直接连接已校验的 IP，抵御 DNS rebinding）。
func newHTTPClient(s ServerConfig) (*http.Client, error) {
	initialURL, err := checkHTTPURL(s.URL, s.AllowInsecureHTTP, s.AllowPrivateNetworks)
	if err != nil {
		return nil, err
	}

	var tlsCfg *tls.Config
	if s.CACertFile != "" {
		pool, err := loadCACertPool(s.CACertFile)
		if err != nil {
			return nil, err
		}
		tlsCfg = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	} else {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	// plainHTTP：该服务器的 URL 是明文 http（allow_insecure_http 已放行）。
	// 明文 http 仍要限制在回环地址，防止一个开关把任意公网明文放行。
	plainHTTP := initialURL.Scheme == "http"
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			if err := dialIPCheck(ip, s, plainHTTP); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, addr)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, resolved := range ips {
			if err := dialIPCheck(resolved.IP, s, plainHTTP); err != nil {
				return nil, err
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no address for host %q", host)
		}
		// 连接已校验的 IP，避免解析结果在拨号时被再次改写（DNS rebinding）。
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}

	client := &http.Client{
		Timeout: s.timeout(),
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DialContext:       dialContext,
			ForceAttemptHTTP2: true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if !sameHTTPOrigin(initialURL, req.URL) {
				return fmt.Errorf("redirect to a different origin is not allowed")
			}
			if req.URL.User != nil {
				return fmt.Errorf("redirect URL user info is not allowed")
			}
			if _, err := checkHTTPURL(req.URL.String(), s.AllowInsecureHTTP, s.AllowPrivateNetworks); err != nil {
				return err
			}
			return nil
		},
	}
	return client, nil
}

// sameHTTPOrigin 比较 scheme、hostname 和有效端口。重定向只能留在服务器配置
// 的同源地址，避免跨源跳转时把自定义认证头转发给另一台服务器。
func sameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || !strings.EqualFold(a.Scheme, b.Scheme) {
		return false
	}
	if canonicalHTTPHost(a.Hostname()) != canonicalHTTPHost(b.Hostname()) {
		return false
	}
	return effectiveHTTPPort(a) == effectiveHTTPPort(b)
}

func canonicalHTTPHost(host string) string {
	host = strings.TrimSuffix(host, ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String()
	}
	return strings.ToLower(host)
}

func effectiveHTTPPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			return "80"
		case "https":
			return "443"
		default:
			return ""
		}
	}
	if number, err := strconv.Atoi(port); err == nil {
		return strconv.Itoa(number)
	}
	return port
}

// loadCACertPool 从文件加载自定义 CA 证书池。
func loadCACertPool(file string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read ca_cert_file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ca_cert_file %q contains no valid certificate", file)
	}
	return pool, nil
}

// buildEnv 构造子进程环境：白名单基础变量 + 显式声明的变量。
// 无 "=" 的条目是畸形 env（Windows/Unix 语义不一致），跳过；
// 重复键按 extra 覆盖白名单透传（map 去重，保持最后写入者胜出）。
func buildEnv(extra, allow []string) []string {
	allowed := make(map[string]struct{}, len(allow))
	for _, k := range baseEnvAllowlist {
		allowed[strings.ToUpper(k)] = struct{}{}
	}
	for _, k := range allow {
		allowed[strings.ToUpper(strings.TrimSpace(k))] = struct{}{}
	}

	extraKVs := make(map[string]string)
	for _, kv := range extra {
		if strings.TrimSpace(kv) == "" {
			continue
		}
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			// 无 "=" 的条目成不了合法环境项，跳过（避免下游平台歧义）。
			continue
		}
		extraKVs[key] = expandEnv(kv)
	}
	seen := make(map[string]struct{}, len(extraKVs))
	out := make([]string, 0, len(extraKVs))
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, override := extraKVs[key]; override {
			continue
		}
		if _, ok := allowed[strings.ToUpper(key)]; ok {
			seen[key] = struct{}{}
			out = append(out, kv)
		}
	}
	for _, kv := range extraKVs {
		out = append(out, kv)
	}
	return out
}

// expandEnv 展开 ${VAR} 形式的环境变量引用（用于凭据/请求头）。
func expandEnv(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.Expand(s, func(key string) string { return os.Getenv(key) })
}

// expandHeaders 展开请求头中的环境变量引用。
func expandHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = expandEnv(v)
	}
	return out
}
