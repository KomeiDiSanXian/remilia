// Package milky 实现了 remilia 机器人框架的 Milky QQ 协议适配器。
//
// Milky 是一套基于 NTQQ 构建的 QQ 机器人协议规范，
// 提供 HTTP API 调用和 WebSocket 事件推送功能。
//
// # 连接模型
//
// Milky 服务端（协议端）运行一个 HTTP 服务器，暴露以下接口：
//   - POST /api/{endpoint} — 动作/API 调用（机器人 → 服务端）
//   - GET  /event          — WebSocket 事件流（服务端 → 机器人）
//
// 本适配器通过 WebSocket 连接 Milky 服务端以接收实时事件，
// 并通过 HTTP POST 调用 /api/* 端点来发送消息及执行其他操作。
//
// # 快速开始
//
//	adapter, err := milky.NewAdapter(milky.Config{
//	    BaseURL:     "http://127.0.0.1:6700",
//	    AccessToken: "your-token",
//	})
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
package milky

import "time"

// Config 保存 Milky 协议适配器的配置项。
type Config struct {
	// BaseURL 是 Milky 服务端的基础 URL（HTTP），例如 "http://127.0.0.1:6700"。
	//
	// 必填。末尾不应带斜杠。
	BaseURL string

	// AccessToken 是用于向 Milky 服务端鉴权的 Bearer 令牌。
	//
	// 可选（若 Milky 服务端不需要鉴权，可留空）。
	// 设置后，所有请求将包含 "Authorization: Bearer {AccessToken}" 请求头。
	AccessToken string

	// WorkerCount 是处理事件的 goroutine 数量。
	//
	// 0 或负数时，默认使用 runtime.NumCPU()。
	WorkerCount int

	// EventBufferSize 是内部事件通道的缓冲区容量。
	//
	// 零值默认为 128。
	EventBufferSize int

	// ReconnectDelay 是两次重连之间的初始等待时间。
	//
	// 零值默认为 3 秒。后续重连采用指数退避，上限为 60 秒。
	ReconnectDelay time.Duration

	// MaxReconnect 是放弃重连前的最大重连次数。
	//
	// 0（零值）表示无限重连（生产环境推荐）。
	MaxReconnect int

	// DialTimeout 是每次 WebSocket 拨号的超时时间。
	//
	// 零值默认为 10 秒。
	DialTimeout time.Duration

	// APITimeout 是每次 HTTP API 调用的超时时间。
	//
	// 零值默认为 15 秒。
	APITimeout time.Duration
}

// DefaultConfig 返回给定基础 URL 的生产环境默认配置。
func DefaultConfig(baseURL string) Config {
	return Config{
		BaseURL:         baseURL,
		WorkerCount:     0, // runtime.NumCPU()
		EventBufferSize: 128,
		ReconnectDelay:  3 * time.Second,
		MaxReconnect:    0, // 无限重连
		DialTimeout:     10 * time.Second,
		APITimeout:      15 * time.Second,
	}
}

// withDefaults 将零值字段填充为合理的默认值。
func (c Config) withDefaults() Config {
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 128
	}
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = 3 * time.Second
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 10 * time.Second
	}
	if c.APITimeout <= 0 {
		c.APITimeout = 15 * time.Second
	}
	return c
}
