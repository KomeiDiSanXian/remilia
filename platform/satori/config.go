package satori

import (
	"fmt"
	"time"
)

// Config 包含连接到 Satori SDK 服务端所需的全部配置。
//
// 示例：
//
//	cfg := satori.Config{
//	    ServerURL:  "http://localhost:5140",
//	    Token:      "my-secret-token",
//	    Platform:   "chronocat",
//	    UserID:     "1234567890",
//	}
//	adapter := satori.NewAdapter(cfg)
type Config struct {
	// ServerURL 是 Satori SDK HTTP/WS 服务端的基础 URL。
	// 示例："http://localhost:5140" 或 "ws://localhost:5140"。
	// Adapter 会自动从该 URL 推导出 WebSocket 地址。
	ServerURL string

	// Token 是 SDK 分发的鉴权令牌。
	// 若 SDK 未配置鉴权，则留空。
	Token string

	// Platform 是请求头中使用的平台标识符（Satori-Platform）。
	// 示例："chronocat"、"lagrange"、"discord"。
	Platform string

	// UserID 是请求头中使用的机器人/账号用户 ID（Satori-User-ID）。
	UserID string

	// Version 是 Satori API 版本号，默认为 "v1"。
	Version string

	// ReconnectDelay 是首次重连前的等待时长。
	// 后续重连使用指数退避，上限为 MaxReconnectDelay。
	// 默认：2 秒。
	ReconnectDelay time.Duration

	// MaxReconnectDelay 是重连退避间隔的上限。
	// 默认：60 秒。
	MaxReconnectDelay time.Duration

	// MaxReconnects 是放弃重连前的最大重试次数。
	// 0 表示无限重连。
	MaxReconnects int

	// EventBufferSize 是内部事件通道的缓冲区大小。
	// 默认：256。
	EventBufferSize int

	// PingInterval 是 WebSocket 上发送 PING 信令的间隔时长。
	// 默认：10 秒（符合 Satori 协议规范要求）。
	PingInterval time.Duration

	// HTTPTimeout 是单次 HTTP API 调用的超时时长。
	// 默认：15 秒。
	HTTPTimeout time.Duration
}

// DefaultConfig 返回针对给定服务端 URL 的合理默认配置。
func DefaultConfig(serverURL, platform, userID string) Config {
	return Config{
		ServerURL:         serverURL,
		Platform:          platform,
		UserID:            userID,
		Version:           "v1",
		ReconnectDelay:    2 * time.Second,
		MaxReconnectDelay: 60 * time.Second,
		MaxReconnects:     0, // 无限重连
		EventBufferSize:   256,
		PingInterval:      10 * time.Second,
		HTTPTimeout:       15 * time.Second,
	}
}

// setDefaults fills zero-value fields with sensible defaults.
func (c *Config) setDefaults() {
	if c.Version == "" {
		c.Version = "v1"
	}
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = 2 * time.Second
	}
	if c.MaxReconnectDelay <= 0 {
		c.MaxReconnectDelay = 60 * time.Second
	}
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 256
	}
	if c.PingInterval <= 0 {
		c.PingInterval = 10 * time.Second
	}
	if c.HTTPTimeout <= 0 {
		c.HTTPTimeout = 15 * time.Second
	}
}

// Validate 检查配置是否合法，若不合法则返回错误。
// 先调用 setDefaults 填充默认值，再校验必填字段。
func (c *Config) Validate() error {
	c.setDefaults()
	if c.ServerURL == "" {
		return fmt.Errorf("satori config: ServerURL cannot be empty")
	}
	if c.Platform == "" {
		return fmt.Errorf("satori config: Platform cannot be empty")
	}
	if c.UserID == "" {
		return fmt.Errorf("satori config: UserID cannot be empty")
	}
	return nil
}
