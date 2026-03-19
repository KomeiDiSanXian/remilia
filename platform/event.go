// Package platform 定义平台无关的消息与事件抽象层。
//
// 设计目标：
//   - 将框架核心（engine、context）与具体平台（QQ官方、Discord、Telegram 等）解耦
//   - 现有 QQ 适配器通过 platform/qq 包实现本接口，向后兼容
//   - 新平台只需实现 PlatformAdapter + Event 接口，无需改动核心引擎
//
// 层次结构：
//
//	┌──────────────┐
//	│   Bot/Engine │  使用 platform.Event / platform.PlatformAdapter
//	├──────────────┤
//	│  platform/   │  接口定义（本包）
//	├──────────────┤
//	│  platform/qq │  QQ 官方实现
//	│  platform/.. │  其他平台实现
//	└──────────────┘
package platform

import "time"

// EventKind 平台无关的事件类别枚举。
//
// 每个平台的具体事件类型（如 dto.EventType）映射到此枚举，
// 供 Engine 的 Matcher 做通用路由（无需感知平台细节）。
type EventKind string

const (
	// EventKindUnknown 未知/未映射事件
	EventKindUnknown EventKind = "UNKNOWN"
	// EventKindPrivateMessage 私聊消息（QQ C2C、Telegram 私聊、Discord DM 等）
	EventKindPrivateMessage EventKind = "PRIVATE_MESSAGE"
	// EventKindGroupMessage 群组/频道消息（QQ 群、Discord 频道等）
	EventKindGroupMessage EventKind = "GROUP_MESSAGE"
	// EventKindGuildMessage 频道/服务器消息（QQ频道、Discord 服务器等）
	EventKindGuildMessage EventKind = "GUILD_MESSAGE"
	// EventKindNotice 通知类事件（入群、退群、好友添加等）
	EventKindNotice EventKind = "NOTICE"
	// EventKindRequest 请求类事件（加好友请求、加群请求等）
	EventKindRequest EventKind = "REQUEST"
	// EventKindSystem 系统事件（Ready、Resumed 等）
	EventKindSystem EventKind = "SYSTEM"
)

// UserInfo 代表消息发送者/用户的基本信息。
//
// 各平台填充能力不同，未知字段返回空字符串或 false。
type UserInfo struct {
	// ID 平台内唯一用户标识（QQ openID、Telegram userID 等）
	ID string
	// DisplayName 用户显示名（昵称/用户名）
	DisplayName string
	// IsBot 是否为机器人账号
	IsBot bool
}

// ChatInfo 代表消息所在会话的基本信息。
type ChatInfo struct {
	// ID 会话/群组/频道唯一标识
	ID string
	// Name 会话名称（可选，部分平台不提供）
	Name string
	// IsGroup 是否为群组/频道消息（false = 私聊）
	IsGroup bool
}

// Event 是平台无关的事件抽象接口。
//
// 各平台适配器将原始 payload 包装为 Event 实现，
// 框架核心只依赖此接口，不直接引用任何平台特定结构体。
type Event interface {
	// Platform 返回平台标识符（如 "qq"、"discord"、"telegram"）
	Platform() string

	// Kind 返回平台无关的事件类别
	Kind() EventKind

	// RawType 返回平台原始事件类型字符串（如 QQ 的 "C2C_MESSAGE_CREATE"）
	RawType() string

	// Sender 返回消息发送者信息
	Sender() UserInfo

	// Chat 返回消息所在会话信息
	Chat() ChatInfo

	// Content 返回消息文本内容（纯文本，不含平台特定格式）
	Content() string

	// Timestamp 返回事件时间戳（尽力而为，平台不提供时返回零值）
	Timestamp() time.Time

	// ID 返回平台级别的唯一事件标识符。
	//
	// 用途：去重、追踪、死信队列等需要唯一标识的场景。
	// 平台不提供时返回空字符串；调用方应对空字符串做兼容处理。
	ID() string

	// RawPayload 返回原始平台 payload（类型断言后可访问平台特定字段）
	//
	// 示例（QQ 平台）:
	//   if payload, ok := e.RawPayload().(*dto.Payload); ok { ... }
	RawPayload() any
}
