package wechat

import (
	"fmt"
	"strings"
	"time"
)

// 企业微信智能机器人的两种接入方式（[Config.Mode]）。
const (
	// ModeWS 长连接：适配器主动连 wss://openws.work.weixin.qq.com 订阅回调。
	// 未显式配置 mode 时的默认值：无需公网地址，无需处理回调加解密。
	ModeWS = "ws"
	// ModeWebhook URL 回调：企业微信把加密回调 POST 到本端，需要公网可达地址。
	ModeWebhook = "webhook"
)

const (
	// defaultEndpoint 是智能机器人长连接的官方地址。
	defaultEndpoint = "wss://openws.work.weixin.qq.com"
	// defaultWebhookPath 是未显式配置时接收回调的路径。
	defaultWebhookPath = "/wechat/aibot"
	// defaultReconnectDelay 是两次重连之间的初始等待时间。
	defaultReconnectDelay = 3 * time.Second
	// defaultDialTimeout 是单次 WebSocket 拨号超时。
	defaultDialTimeout = 10 * time.Second
	// defaultRequestTimeout 是单次信令"写出去到收到回执"的超时。
	defaultRequestTimeout = 15 * time.Second
	// defaultEventBuffer 是内部事件通道的默认容量。
	defaultEventBuffer = 256
	// defaultWorkers 是事件处理协程的默认数量。
	defaultWorkers = 4
	// defaultShutdownTimeout 是 webhook 优雅关闭的默认等待时长。
	defaultShutdownTimeout = 10 * time.Second
	// defaultMaxReconnectDelay 是重连退避上限。
	defaultMaxReconnectDelay = 60 * time.Second
)

// Config 企业微信智能机器人适配器配置。
//
// 两种接入方式共用同一份配置，仅传输层不同：
//   - ws（默认）：只需 BotID + Secret；主动推送、媒体上传、模板卡片全可用。
//   - webhook：需要 Token + EncodingAESKey；回复只能走回调里的 response_url
//     （一次性、1 小时有效），无法主动推送与上传媒体。
//
// 两种模式的凭证互不通用：Secret 是长连接专用密钥，Token/EncodingAESKey
// 只在 URL 回调模式下存在（见企业微信「智能机器人 - API 模式」配置页）。
type Config struct {
	// Mode 接入方式："ws"（默认）或 "webhook"。
	Mode string

	// BotID 智能机器人 BotID，两种模式都用于标识机器人自身。
	BotID string

	// Secret 长连接专用密钥，仅 ws 模式必填。
	Secret string

	// Token 回调签名 Token，仅 webhook 模式必填。
	Token string

	// EncodingAESKey 回调加密密钥（43 位），仅 webhook 模式必填。
	EncodingAESKey string

	// Endpoint 长连接地址，留空使用官方地址（测试可覆盖）。
	Endpoint string

	// Webhook URL 回调模式的监听参数，仅 webhook 模式生效。
	Webhook WebhookConfig

	// ReconnectDelay 重连初始退避，零值默认 3s，之后指数增长。
	ReconnectDelay time.Duration

	// MaxReconnect 连续重连次数上限，0 表示无限重连（生产推荐）。
	MaxReconnect int

	// DialTimeout 单次 WebSocket 拨号超时，零值默认 10s。
	DialTimeout time.Duration

	// RequestTimeout 单次信令等待回执的超时，零值默认 15s。
	RequestTimeout time.Duration

	// EventBuffer 内部事件通道容量，零值默认 256。
	EventBuffer int

	// WorkerCount 事件处理协程数，零值默认 4。
	WorkerCount int
}

// WebhookConfig URL 回调模式的监听参数。
type WebhookConfig struct {
	// ListenAddr HTTP 监听地址，如 ":8080"。
	ListenAddr string

	// Path 接收回调的路径，默认 "/wechat/aibot"。
	Path string

	// ShutdownTimeout 优雅关闭等待时长，默认 10s。
	ShutdownTimeout time.Duration
}

// ResolveMode 返回规范化的接入方式：留空时按已填凭证推断。
//
// 只填了 Token/EncodingAESKey 而没填 Secret 时按 webhook 处理，
// 避免把一份回调配置误当成"ws 缺 Secret"而启动失败。
func (c Config) ResolveMode() string {
	switch c.Mode {
	case ModeWS, ModeWebhook:
		return c.Mode
	case "":
		if c.Secret == "" && (c.Token != "" || c.EncodingAESKey != "") {
			return ModeWebhook
		}
		return ModeWS
	default:
		return c.Mode
	}
}

// setDefaults 填充零值字段。
func (c *Config) setDefaults() {
	if c.Endpoint == "" {
		c.Endpoint = defaultEndpoint
	}
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = defaultReconnectDelay
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = defaultDialTimeout
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = defaultRequestTimeout
	}
	if c.EventBuffer <= 0 {
		c.EventBuffer = defaultEventBuffer
	}
	if c.WorkerCount <= 0 {
		c.WorkerCount = defaultWorkers
	}
	if c.Webhook.Path == "" {
		c.Webhook.Path = defaultWebhookPath
	}
	if !strings.HasPrefix(c.Webhook.Path, "/") {
		c.Webhook.Path = "/" + c.Webhook.Path
	}
	if c.Webhook.ShutdownTimeout <= 0 {
		c.Webhook.ShutdownTimeout = defaultShutdownTimeout
	}
}

// validate 校验当前接入方式所需的凭证是否齐备。
func (c *Config) validate() error {
	switch c.ResolveMode() {
	case ModeWS:
		if c.Secret == "" {
			return fmt.Errorf("wechat adapter: ws 模式需要 secret（长连接专用密钥）")
		}
	case ModeWebhook:
		if c.Token == "" {
			return fmt.Errorf("wechat adapter: webhook 模式需要 token（回调签名 Token）")
		}
		if c.EncodingAESKey == "" {
			return fmt.Errorf("wechat adapter: webhook 模式需要 encoding_aes_key（43 位）")
		}
		if _, err := newWXCrypt(c.Token, c.EncodingAESKey, ""); err != nil {
			return err
		}
		if c.Webhook.ListenAddr == "" {
			return fmt.Errorf("wechat adapter: webhook 模式需要 webhook.listen_addr")
		}
	default:
		return fmt.Errorf("wechat adapter: 不支持的 mode %q", c.Mode)
	}
	return nil
}
