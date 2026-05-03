package onebot

import "time"

// ────────────────────────────────────────────────────────────────────────────
// 连接模式
// ────────────────────────────────────────────────────────────────────────────

// Mode 控制适配器与 OneBot 实现的通信方式。
type Mode int

const (
	// ModeForwardWS （默认）：适配器主动连接 OneBot WS 服务端。
	// OneBot 实现须开启 ws.enable = true 并暴露 WS 端点。
	ModeForwardWS Mode = iota

	// ModeReverseWS ：适配器监听；由 OneBot 实现反向连接到适配器。
	// OneBot 实现须开启 ws_reverse.enable = true。
	ModeReverseWS

	// ModeHTTPPost ：适配器监听 HTTP POST 事件上报。
	// OneBot 实现须开启 http_post.enable = true。
	// API 调用通过 HTTP 发往 OneBot HTTP 服务器（需要配置 APIURL）。
	ModeHTTPPost
)

// ────────────────────────────────────────────────────────────────────────────
// Config
// ────────────────────────────────────────────────────────────────────────────

// Config 保存 OneBot V11 适配器的所有配置项。
type Config struct {
	// Mode 选择通信方式，默认为 ModeForwardWS。
	Mode Mode

	// ── 正向 WS 和 HTTP POST API 模式 ──────────────────────────────────────

	// URL 是 OneBot 实现的 WebSocket 或 HTTP 基础地址。
	//
	// ForwardWS：  ws://127.0.0.1:6700  （连接到 /）
	// HTTPPost：   http://127.0.0.1:5700 （API 调用通过 HTTP 发往此地址）
	URL string

	// ── 反向 WS 和 HTTP POST 服务器 ────────────────────────────────────────

	// ListenAddr 是本地监听的 address:port。
	//
	// ReverseWS：  ":8080"（OneBot 连接到 ws://host:8080/）
	// HTTPPost：   ":8080"（OneBot 向 http://host:8080/ POST 事件）
	ListenAddr string

	// ── 鉴权 ────────────────────────────────────────────────────────────────

	// Token 是访问令牌（Authorization: Bearer <token>）。
	// 当 OneBot 实现配置了 access_token 时需要设置。
	Token string

	// Secret 是 HTTP POST 事件验签的 HMAC-SHA1 密钥。
	// 仅在 ModeHTTPPost 模式下使用。
	Secret string

	// ── 时间参数 ─────────────────────────────────────────────────────────────

	// ReconnectDelay 是第一次重连前的初始等待时间。
	// 后续重连使用指数退避，上限为 ReconnectMaxDelay。
	// 默认值：1 秒。
	ReconnectDelay time.Duration

	// ReconnectMaxDelay 限制指数退避的最大等待时间，默认值：60 秒。
	ReconnectMaxDelay time.Duration

	// APITimeout 是每次 API 请求的超时时间，默认值：10 秒。
	APITimeout time.Duration

	// EventBufferSize 是接收事件的通道缓冲区大小，默认值：100。
	EventBufferSize int

	// ── 心跳 ─────────────────────────────────────────────────────────────────

	// HeartbeatInterval 是适配器检查连接健康状态的间隔。
	// 设为 0 则禁用心跳 goroutine，默认值：0。
	HeartbeatInterval time.Duration
}

// DefaultConfig 返回适合连接本地默认 go-cqhttp / NapCat 实例（ForwardWS 模式）的 Config。
func DefaultConfig(url string) Config {
	return Config{
		Mode:              ModeForwardWS,
		URL:               url,
		ReconnectDelay:    1 * time.Second,
		ReconnectMaxDelay: 60 * time.Second,
		APITimeout:        10 * time.Second,
		EventBufferSize:   100,
	}
}

// DefaultReverseConfig 返回反向 WebSocket 模式的 Config。
func DefaultReverseConfig(listenAddr string) Config {
	return Config{
		Mode:            ModeReverseWS,
		ListenAddr:      listenAddr,
		APITimeout:      10 * time.Second,
		EventBufferSize: 100,
	}
}

// DefaultHTTPPostConfig 返回 HTTP POST 模式的 Config。
//
// listenAddr 是适配器接收事件 POST 的监听地址（如 ":8080"）。
// apiURL 是 OneBot HTTP API 服务器的基础地址（如 "http://127.0.0.1:5700"）。
func DefaultHTTPPostConfig(listenAddr, apiURL string) Config {
	return Config{
		Mode:            ModeHTTPPost,
		ListenAddr:      listenAddr,
		URL:             apiURL,
		APITimeout:      10 * time.Second,
		EventBufferSize: 100,
	}
}

// reconnectDelay 返回已配置的延迟时间，若未配置则使用合理默认值。
func (c *Config) reconnectDelay() time.Duration {
	if c.ReconnectDelay <= 0 {
		return time.Second
	}
	return c.ReconnectDelay
}

// reconnectMaxDelay 返回已配置的最大延迟时间，若未配置则使用合理默认值。
func (c *Config) reconnectMaxDelay() time.Duration {
	if c.ReconnectMaxDelay <= 0 {
		return 60 * time.Second
	}
	return c.ReconnectMaxDelay
}

// apiTimeout 返回已配置的超时时间，若未配置则使用合理默认值。
func (c *Config) apiTimeout() time.Duration {
	if c.APITimeout <= 0 {
		return 10 * time.Second
	}
	return c.APITimeout
}

// eventBufferSize 返回已配置的缓冲区大小，若未配置则使用合理默认值。
func (c *Config) eventBufferSize() int {
	if c.EventBufferSize <= 0 {
		return 100
	}
	return c.EventBufferSize
}
