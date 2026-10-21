package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/outbound"
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
// 明文 http 默认要求 allow_insecure_http；目标地址默认只允许回环，
// 显式开启 allow_private_networks 后私网地址也随之允许（自建 MCP 服务器
// 通常就在局域网里、只能走明文），公网明文始终拒绝——此前只查 scheme，
// 一个开关就把任意公网明文放行了。
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
		if u.Scheme == "http" && !plainHTTPDestinationAllowed(ip, allowPrivate) {
			return nil, fmt.Errorf("plain http is restricted to loopback addresses (or private networks with allow_private_networks), got %s", ip)
		}
		if err := forbidIP(ip, allowInsecure, allowPrivate); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// plainHTTPDestinationAllowed 判断明文 http 的目标地址是否在允许范围内：
// 回环地址（allow_insecure_http 的原有语义），以及显式开启
// allow_private_networks 后的私网地址。公网明文永远不允许，避免"一个开关
// 就放行任意公网明文"。
func plainHTTPDestinationAllowed(ip net.IP, allowPrivate bool) bool {
	if ip.IsLoopback() {
		return true
	}
	return allowPrivate && ip.IsPrivate()
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

// checkDestinationIP 校验目标 IP 是否在允许范围内：SSRF 防护 + 明文 http
// 的目标限制。启动期解析主机名后逐个地址过一遍（运行期不再逐请求校验，
// 见 newHTTPClient 的说明）。
func checkDestinationIP(ip net.IP, s ServerConfig, plainHTTP bool) error {
	if err := forbidIP(ip, s.AllowInsecureHTTP, s.AllowPrivateNetworks); err != nil {
		return err
	}
	if plainHTTP && !plainHTTPDestinationAllowed(ip, s.AllowPrivateNetworks) {
		return fmt.Errorf("plain http is restricted to loopback addresses (or private networks with allow_private_networks), got %s", ip)
	}
	return nil
}

// checkResolvedHost 在启动期解析主机名一次，并要求所有解析结果都在允许范围内。
//
// 此前这一步发生在每次拨号前，代价是：内网自建 MCP 服务器（运维自己配置的
// 目标）在每次请求时都要重新过一遍 SSRF 判定，一旦实现侧换了解析结果或
// 配置漏了 allow_private_networks，就表现为"连不上/请求被拒"。端点由运维
// 写死在配置里，攻击者影响不了这段配置，因此启动期校验一次就够——运行期
// 的信任决定见 newHTTPClient。
func checkResolvedHost(ctx context.Context, u *url.URL, s ServerConfig) error {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return fmt.Errorf("resolve %q: %w", u.Hostname(), err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no address for host %q", u.Hostname())
	}
	plainHTTP := u.Scheme == "http"
	for _, resolved := range ips {
		if err := checkDestinationIP(resolved.IP, s, plainHTTP); err != nil {
			return err
		}
	}
	return nil
}

// newHTTPClient 构造 MCP 服务器客户端，策略交给 infra/outbound：
//
//   - 类为 OperatorEndpoint：服务器地址由运维配置，命中该源即放行内网/明文，
//     运行期不再逐请求做公网校验（那只会误伤自建服务器，挡不住真正的攻击者）；
//   - 启动期仍完整校验：scheme、明文开关、字面 IP / 主机名解析结果；
//   - 重定向必须同源（MCP 会带自定义认证头，跨源跳转会把它转发给另一台服务器）；
//   - 强制 TLS 1.2+，支持 ca_cert_file；
//   - 代理沿用 HTTP(S)_PROXY（此前该客户端完全不读代理，需要代理的部署里
//     MCP 一律连不上）。
func newHTTPClient(s ServerConfig) (*outbound.Client, error) {
	initialURL, err := checkHTTPURL(s.URL, s.AllowInsecureHTTP, s.AllowPrivateNetworks)
	if err != nil {
		return nil, err
	}
	if net.ParseIP(initialURL.Hostname()) == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := checkResolvedHost(ctx, initialURL, s); err != nil {
			return nil, err
		}
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

	return outbound.New(outbound.Policy{
		Component:      "mcp." + s.Name,
		Class:          outbound.OperatorEndpoint,
		Origins:        []string{initialURL.String()},
		Timeout:        s.timeout(),
		MaxRedirects:   5,
		TLSConfig:      tlsCfg,
		SameOriginOnly: true,
	})
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
