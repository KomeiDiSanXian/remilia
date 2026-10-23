// Package outbound 集中构造"出站 HTTP 客户端"，把三件事绑在一起：
//
//  1. **信任分层**：按目标来源把请求分成 [Untrusted]、[TrustedOrigin]、
//     [OperatorEndpoint] 三类，各自一套准入策略；
//  2. **执行层兜底**：代理解析、拨号前二次校验（防 DNS 重绑定）、逐跳
//     重定向校验；
//  3. **可观测性**：每次拦截都进指标并限频告警——此前策略误伤往往只在
//     debug 日志里留一行，运维看到的现象是"功能坏了"而不是"被策略拦了"。
//
// # 为什么必须分层
//
// 出站目标的性质不同，适用的策略也不同：
//
//   - 用户/模型给出的 URL（/fetch、fetch_url、模型指定的图片）是**不可信**
//     目标：只允许 https + 公网，SSRF 防护必须完整；
//   - 运维写在配置里的端点（自建 SearXNG、MCP 服务器、图片生成后端）是
//     **运维的信任决定**：它们常位于回环/内网并走明文，套用公网校验只会
//     误伤，并不会提升安全（攻击者影响不了这段配置）；
//   - 平台/框架自己产生的地址（适配器托管的媒体源）介于两者之间：只信任
//     适配器声明的那几个源，跨源仍按不可信处理。
//
// 把这三类混在一个共享 Transport 上，就会出现两种错位：给搜索端点开了
// 口子、抓取路径跟着变松；或者某个路径忘了加名单，整块功能不可用。因此
// 本包要求调用方**显式选类**，策略在构造期固化（不在请求期改共享字段，
// 并行调用无数据竞争）。
//
// # 用法
//
//	client, err := outbound.New(outbound.Policy{
//		Component: "websearch.fetch",
//		Class:     outbound.Untrusted,
//		Timeout:   15 * time.Second,
//	})
//	if err != nil { ... }
//	if err := client.Allow(rawURL); err != nil { ... } // 可选：请求前准入
//	resp, err := client.Get(ctx, rawURL)
//
// 用 [Blocked] 判断错误是否由策略拦截产生（而不是网络故障），据此给出
// 可读提示或上报。
package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/netguard"
)

// Class 是出站目标的信任等级。零值为 [Untrusted]，忘记显式选类时落到最严策略。
type Class int

const (
	// Untrusted 目标由用户、模型或第三方给出，内容不可信：
	// 仅 https、仅公网、禁止 URL 用户信息。
	Untrusted Class = iota

	// TrustedOrigin 目标可能来自框架/平台侧（平台附件、图片后端回给调用方的
	// 地址）：命中所声明源（Origins）的地址放行内网与明文，跨源仍按
	// [Untrusted] 处理。Origins 为空时退化为严格策略。
	TrustedOrigin

	// OperatorEndpoint 目标由运维写在配置里（搜索实例、MCP 服务器）：
	// 语义与 [TrustedOrigin] 相同，但要求 Origins 非空——写错类（配了端点却
	// 没声明源）在构造期就报错，而不是等到运行期才发现请求全被拦。
	//
	// 该类的 Origins 只能来自运维配置或适配器的自声明，**绝不能**由用户输入
	// 拼装：命中即等于对该目标开放直连。
	OperatorEndpoint
)

// String 返回类的稳定名称（用于日志与指标）。
func (c Class) String() string {
	switch c {
	case Untrusted:
		return "untrusted"
	case TrustedOrigin:
		return "trusted-origin"
	case OperatorEndpoint:
		return "operator-endpoint"
	default:
		return "class-" + itoa(int(c))
	}
}

// Reason 是一次策略拦截的原因，用于指标标签与错误信息。
type Reason string

const (
	// ReasonMalformed URL 无法解析或缺少主机名。
	ReasonMalformed Reason = "malformed"
	// ReasonUserInfo URL 携带用户信息（user:pass@host），可能是凭据注入。
	ReasonUserInfo Reason = "userinfo"
	// ReasonScheme 协议不是 http/https。
	ReasonScheme Reason = "scheme"
	// ReasonPlainHTTP 严格路径收到明文 http 目标。
	ReasonPlainHTTP Reason = "plain-http"
	// ReasonPrivateAddress 目标解析到内网/保留地址（含云元数据地址）。
	ReasonPrivateAddress Reason = "private-address"
	// ReasonRedirects 重定向跳数超限。
	ReasonRedirects Reason = "too-many-redirects"
	// ReasonCrossOrigin 要求同源的场景下跳到了别的源。
	ReasonCrossOrigin Reason = "cross-origin-redirect"
)

// message 返回面向使用者的说明（会出现在命令回复/工具结果里）。
func (r Reason) message() string {
	switch r {
	case ReasonMalformed:
		return "链接格式无效"
	case ReasonUserInfo:
		return "链接不允许携带用户信息"
	case ReasonScheme:
		return "仅支持 http/https 链接"
	case ReasonPlainHTTP:
		return "仅支持 https 链接"
	case ReasonPrivateAddress:
		return "目标地址不是公网地址（SSRF 防护）"
	case ReasonRedirects:
		return "重定向次数过多"
	case ReasonCrossOrigin:
		return "重定向到不同源的目标被拒绝"
	default:
		return "目标地址不被允许"
	}
}

// Policy 描述一个出站客户端的策略。字段都有安全默认值，只需填 Component、
// Class 与（需要时）Origins。
type Policy struct {
	// Component 调用方标识（如 "websearch.fetch"），进指标与日志。
	Component string
	// Class 信任等级，零值即 [Untrusted]。
	Class Class
	// Origins 受信来源，写法与 netguard.HostAllowlist 一致：
	// "host"（该主机任意端口）、"host:port"（精确）、或完整 URL。
	// 仅 [TrustedOrigin]、[OperatorEndpoint] 使用。
	Origins []string
	// Proxy 显式代理地址；空值时沿用环境变量（HTTP(S)_PROXY / NO_PROXY）。
	Proxy string
	// Timeout 整次请求（含重定向）的超时；0 表示不设总超时，由 ctx 控制。
	Timeout time.Duration
	// MaxRedirects 最大重定向跳数；0 取默认值 10。
	MaxRedirects int
	// TLSConfig 自定义 TLS 配置（如 MCP 的自签 CA）。nil 用系统根证书。
	TLSConfig *tls.Config
	// SameOriginOnly 要求重定向只能停留在 Origins[0] 的同源地址（MCP 用：
	// 跨源跳转会把自定义认证头带给另一台服务器）。启用时 Origins[0] 必须是
	// 完整 URL。
	SameOriginOnly bool
}

// defaultMaxRedirects 未配置时的重定向上限。
const defaultMaxRedirects = 10

// Validate 校验策略本身是否自洽。导出以便配置层在启动期提前发现写错。
func (p Policy) Validate() error {
	switch p.Class {
	case Untrusted, TrustedOrigin, OperatorEndpoint:
	default:
		return fmt.Errorf("outbound: unknown class %d", int(p.Class))
	}
	if p.Class == Untrusted && len(p.Origins) > 0 {
		return errors.New("outbound: Untrusted class must not declare origins")
	}
	if p.Class == OperatorEndpoint && len(p.Origins) == 0 {
		return errors.New("outbound: OperatorEndpoint class requires at least one origin")
	}
	for _, origin := range p.Origins {
		if strings.TrimSpace(origin) == "" {
			return errors.New("outbound: empty origin entry")
		}
		if _, ok := netguard.DialAddr(origin); ok {
			continue
		}
		if netguard.ValidHostEntry(origin) {
			continue
		}
		return fmt.Errorf("outbound: invalid origin %q (want host, host:port or http(s) URL)", origin)
	}
	if p.SameOriginOnly {
		if len(p.Origins) == 0 {
			return errors.New("outbound: SameOriginOnly requires an origin")
		}
		u, err := url.Parse(strings.TrimSpace(p.Origins[0]))
		if err != nil || u.Hostname() == "" {
			return errors.New("outbound: SameOriginOnly requires the first origin to be an absolute URL")
		}
	}
	if p.Proxy != "" {
		if _, err := url.Parse(p.Proxy); err != nil {
			return fmt.Errorf("outbound: invalid proxy %q: %w", p.Proxy, err)
		}
	}
	return nil
}

// Client 是带策略的 HTTP 客户端。零值不可用，必须经 [New] 构造。
type Client struct {
	http         *http.Client
	policy       Policy
	hosts        netguard.HostAllowlist
	origin0      *url.URL
	proxyAddrs   netguard.HostAllowlist
	dialer       *net.Dialer
	maxRedirects int
}

// New 按策略构造客户端。策略非法或代理地址无法解析时返回错误。
func New(p Policy) (*Client, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Component) == "" {
		p.Component = "unnamed"
	}
	c := &Client{
		policy:       p,
		hosts:        netguard.HostAllowlist(p.Origins),
		dialer:       &net.Dialer{Timeout: 30 * time.Second},
		maxRedirects: p.MaxRedirects,
	}
	if c.maxRedirects <= 0 {
		c.maxRedirects = defaultMaxRedirects
	}
	if p.SameOriginOnly && len(p.Origins) > 0 {
		c.origin0, _ = url.Parse(strings.TrimSpace(p.Origins[0]))
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	if p.Proxy != "" {
		proxy, err := url.Parse(p.Proxy)
		if err != nil {
			return nil, fmt.Errorf("outbound: invalid proxy %q: %w", p.Proxy, err)
		}
		tr.Proxy = http.ProxyURL(proxy)
	}
	if p.TLSConfig != nil {
		tr.TLSClientConfig = p.TLSConfig
	}
	c.proxyAddrs = proxyAllowlist(tr)
	tr.DialContext = c.dialContext

	c.http = &http.Client{
		Timeout:       p.Timeout,
		Transport:     tr,
		CheckRedirect: c.checkRedirect,
	}
	logPolicy(p)
	return c, nil
}

// Allows 只做策略判定而不发起请求（不建客户端、不占连接池），供"只校验不
// 下载"的路径使用（例如模型给出的图片地址要交给平台发送）。拦截同样计入
// 指标与限频告警。
func Allows(rawURL string, p Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(p.Component) == "" {
		p.Component = "unnamed"
	}
	c := &Client{policy: p, hosts: netguard.HostAllowlist(p.Origins)}
	return c.Allow(rawURL)
}

// MustNew 是 [New] 的 panic 版本，只用于策略为编译期常量的包级初始化：
// 静态策略校验失败属于编程错误，应在启动时就炸掉而不是静默退化。
func MustNew(p Policy) *Client {
	c, err := New(p)
	if err != nil {
		panic("outbound: invalid static policy: " + err.Error())
	}
	return c
}

// Class 返回客户端的信任等级（测试与诊断用）。
func (c *Client) Class() Class { return c.policy.Class }

// Allow 执行请求前准入：URL 不合法或目标不可信时返回 [*BlockError]，并计入
// 拦截指标。它不发起网络请求，因此可用于"只校验不下载"的场景。
func (c *Client) Allow(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return c.block(ReasonMalformed, rawURL)
	}
	return c.allowURL(u)
}

// Get 发起一次 GET 请求（先过准入）。
func (c *Client) Get(ctx context.Context, rawURL string) (*http.Response, error) {
	if err := c.Allow(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

// CloseIdleConnections 释放底层 Transport 的空闲连接（传输关闭时调用）。
func (c *Client) CloseIdleConnections() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}

// Clone 返回共享同一策略、拨号守卫与重定向判定的派生客户端，edit 可调整
// 派生客户端自身的参数（例如清空整体超时、替换 Transport 的
// ResponseHeaderTimeout）。edit 不得改写 Transport 的 DialContext /
// TLSClientConfig，否则等于绕过策略。
func (c *Client) Clone(edit func(*http.Client)) *Client {
	clone := *c
	h := *c.http
	if edit != nil {
		edit(&h)
	}
	clone.http = &h
	return &clone
}

// Do 执行请求。调用方若需要请求前准入，先调用 [Client.Allow]。
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := c.http.Do(req)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	recordRequest(c.policy.Component, time.Since(start), status, err)
	return resp, err
}

// allowURL 是准入的公共实现：受信源直接放行，其余按 https + 公网校验。
func (c *Client) allowURL(u *url.URL) error {
	if u == nil {
		return c.block(ReasonMalformed, "")
	}
	if u.User != nil {
		return c.block(ReasonUserInfo, urlString(u))
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		return c.block(ReasonMalformed, urlString(u))
	}
	if scheme != "http" && scheme != "https" {
		return c.block(ReasonScheme, urlString(u))
	}
	if u.Hostname() == "" {
		return c.block(ReasonMalformed, urlString(u))
	}
	if c.hosts.Contains(u) {
		// 受信源：允许内网与明文，与声明者的信任决定一致。
		return nil
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return c.block(ReasonPlainHTTP, urlString(u))
	}
	if !netguard.IsPublicHost(u.Hostname()) {
		return c.block(ReasonPrivateAddress, urlString(u))
	}
	return nil
}

// checkRedirect 逐跳校验重定向目标：跳数、同源约束、目标准入。
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= c.maxRedirects {
		return c.block(ReasonRedirects, urlString(req.URL))
	}
	if c.origin0 != nil && !netguard.SameOrigin(c.origin0, req.URL) {
		return c.block(ReasonCrossOrigin, urlString(req.URL))
	}
	return c.allowURL(req.URL)
}

// dialContext 是执行层兜底：
//
//   - 目标是已配置代理 → 直连代理（代理通常在本机/内网，公网校验只会误伤；
//     目标由代理解析，不在这里判断）；
//   - 目标是受信源 → 直接拨号（信任决定已经由策略作出）；
//   - 其余目标 → 解析后要求**全部**结果为公网 IP，再拨号到已校验的 IP，
//     把"校验后 DNS 重绑定"的窗口关掉（而不是仅缩小）。
func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if c.proxyAddrs.ContainsAddr(addr) || c.hosts.ContainsAddr(addr) {
		return c.dialer.DialContext(ctx, network, addr)
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP address found for host %q", host)
	}
	for _, ip := range ips {
		if !netguard.IsPublicIP(ip) {
			return nil, c.block(ReasonPrivateAddress, "dial://"+addr)
		}
	}
	var firstErr error
	for _, ip := range ips {
		conn, derr := c.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = derr
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, firstErr
}

// BlockError 表示请求被策略拦截。用 [Blocked] 提取原因。
type BlockError struct {
	Component string
	Class     Class
	Reason    Reason
	URL       string
}

// Error 实现 error；只暴露原因，不暴露内部组件名（会直接展示给用户）。
func (e *BlockError) Error() string { return e.Reason.message() }

// Blocked 判断 err 是否由出站策略拦截产生，并返回拦截原因。
func Blocked(err error) (Reason, bool) {
	if be, ok := errors.AsType[*BlockError](err); ok {
		return be.Reason, true
	}
	return "", false
}

// block 记录一次拦截并返回错误。
func (c *Client) block(r Reason, raw string) error {
	be := &BlockError{
		Component: c.policy.Component,
		Class:     c.policy.Class,
		Reason:    r,
		URL:       sanitize(raw),
	}
	recordBlocked(c.policy.Component, r)
	logBlocked(c.policy.Component, r, be.URL)
	return be
}

// proxyAllowlist 收集 Transport 可能拨号到的代理地址（显式配置 + 环境变量），
// 归一化为 "host:port" 精确条目。
func proxyAllowlist(tr *http.Transport) netguard.HostAllowlist {
	var out netguard.HostAllowlist
	add := func(u *url.URL) {
		if u == nil || u.Hostname() == "" {
			return
		}
		port := u.Port()
		if port == "" {
			port = "80"
			if strings.EqualFold(u.Scheme, "https") {
				port = "443"
			}
		}
		out = append(out, net.JoinHostPort(strings.ToLower(u.Hostname()), port))
	}
	if tr.Proxy != nil {
		probe := &http.Request{URL: &url.URL{Scheme: "https", Host: "outbound-probe.invalid"}}
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
	return out
}

// logPolicy 在构造期记录一次策略决策：运维不必等到"功能不可用"才知道
// 某个端点被归到了哪一类。
func logPolicy(p Policy) {
	if p.Class == Untrusted && len(p.Origins) == 0 {
		return
	}
	logger.Infof("[outbound] component=%s class=%s origins=%s", p.Component, p.Class, strings.Join(p.Origins, ","))
}

// urlString 安全地把 URL 转成字符串（nil 安全）。
func urlString(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}

// sanitize 去掉查询串与用户信息，避免把令牌写进日志/指标。
func sanitize(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		if len(raw) > 200 {
			return raw[:200] + "…"
		}
		return raw
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	s := u.String()
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// itoa 很小的整数格式化，避免为日志引入 strconv 依赖。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
