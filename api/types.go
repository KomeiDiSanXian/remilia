package api

import "github.com/KomeiDiSanXian/remilia/plugin"

// BotInfo 是 Bot 实例的公开摘要信息，用于列表和详情响应。
type BotInfo struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`              // "running" | "stopped"
	Uptime      string   `json:"uptime"`              // 人类可读的运行时长
	Version     string   `json:"version"`             // 框架版本
	Platforms   []string `json:"platforms,omitempty"` // 已接入的聊天平台列表
	PluginCount int      `json:"plugin_count"`        // 已注册插件数量
}

// PluginInfo 是插件的公开摘要信息，用于列表响应。
// 详情响应直接使用 plugin.Status，此处仅用于列表场景的轻量结构。
type PluginInfo struct {
	Name         string   `json:"name"`
	State        string   `json:"state"`                  // Loaded | Disabled | Error 等
	Version      string   `json:"version"`                // 插件版本
	Uptime       string   `json:"uptime"`                 // 运行时长
	Dependencies []string `json:"dependencies,omitempty"` // 依赖的其他插件
	MatcherCount int      `json:"matcher_count"`          // 注册的匹配器数量
	LastError    string   `json:"last_error,omitempty"`   // 最后错误信息
	LoadTime     string   `json:"load_time,omitempty"`    // RFC3339 格式的加载时间
}

// VersionInfo 是版本信息响应。
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`     // Git commit SHA
	BuildDate string `json:"build_date,omitempty"` // 构建时间
	GoVersion string `json:"go_version"`           // Go 运行时版本
}

// PluginDetail 是插件详情响应。
//
// plugin.Status 直接序列化会暴露 Go 内部表示（State 为 int 枚举、LastError 为
// error 接口、Uptime 为纳秒整数），此处转换为前端可直接展示的稳定形状。
// Metadata 沿用 plugin.Metadata 原样输出（字段名为 Go 风格）。
type PluginDetail struct {
	Name                  string           `json:"name"`
	State                 string           `json:"state"`  // Loaded | Disabled | Error 等
	Uptime                string           `json:"uptime"` // 人类可读的运行时长
	MatcherCount          int              `json:"matcher_count"`
	LastError             string           `json:"last_error,omitempty"`
	LoadTime              string           `json:"load_time,omitempty"` // RFC3339
	HasSaveState          bool             `json:"has_save_state"`
	EventBusSubscriptions int              `json:"event_bus_subscriptions"`
	GoroutineCount        int              `json:"goroutine_count"`
	Metadata              *plugin.Metadata `json:"metadata,omitempty"`
}
