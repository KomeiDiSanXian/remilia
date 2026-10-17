package context

// event_types.go — 常用事件类型常量
//
// 插件开发者在注册命令时无需 import "platform"，
// 直接使用这些常量作为 eventType 参数即可。
//
// 这些常量由 platform.EventKind 派生（而不是字面量副本）：枚举改名、
// 新增或调整取值时，这里会随之变化或在编译期暴露，不会再出现"字面量
// 与平台枚举静默失配"的情况。
//
// 使用示例：
//
//	ctx.OnCommand(context.EventPrivate, "/ping", handler)
//	ctx.OnCommand(context.EventGroup, "/ban", handler)
//	ctx.OnCommand(context.EventAll, "/echo", handler)

import "github.com/KomeiDiSanXian/remilia/platform"

// 预定义事件类型常量，与 platform.EventKind 值一致。
const (
	// EventAll 匹配所有事件类型（默认值）。
	EventAll = ""

	// EventPrivate 私聊消息（QQ C2C、Telegram 私聊、Discord DM 等）。
	EventPrivate = string(platform.EventKindPrivateMessage)

	// EventGroup 群组消息（QQ 群、Discord 频道等）。
	EventGroup = string(platform.EventKindGroupMessage)

	// EventGuild 频道/服务器消息（QQ频道、Discord 服务器等）。
	EventGuild = string(platform.EventKindGuildMessage)

	// EventNotice 通知类事件。
	EventNotice = string(platform.EventKindNotice)

	// EventInteraction 交互事件（按钮回调、斜杠命令等）。
	EventInteraction = string(platform.EventKindInteraction)

	// EventReaction 消息表情回应。
	EventReaction = string(platform.EventKindReaction)

	// EventMemberJoin 成员加入群组/服务器。
	EventMemberJoin = string(platform.EventKindMemberJoin)

	// EventMemberLeave 成员离开群组/服务器。
	EventMemberLeave = string(platform.EventKindMemberLeave)

	// EventMessageUpdate 消息编辑。
	EventMessageUpdate = string(platform.EventKindMessageUpdate)

	// EventMessageDelete 消息撤回/删除。
	EventMessageDelete = string(platform.EventKindMessageDelete)
)
