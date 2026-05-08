package context

// event_types.go — 常用事件类型常量
//
// 插件开发者在注册命令时无需 import "platform"，
// 直接使用这些常量作为 eventType 参数即可。
//
// 使用示例：
//
//	ctx.OnCommand(context.EventPrivate, "/ping", handler)
//	ctx.OnCommand(context.EventGroup, "/ban", handler)
//	ctx.OnCommand(context.EventAll, "/echo", handler)

// 预定义事件类型常量，与 platform.EventKind 值一致。
const (
	// EventAll 匹配所有事件类型（默认值）。
	EventAll = ""

	// EventPrivate 私聊消息（QQ C2C、Telegram 私聊、Discord DM 等）。
	EventPrivate = "PRIVATE_MESSAGE"

	// EventGroup 群组消息（QQ 群、Discord 频道等）。
	EventGroup = "GROUP_MESSAGE"

	// EventGuild 频道/服务器消息（QQ频道、Discord 服务器等）。
	EventGuild = "GUILD_MESSAGE"

	// EventNotice 通知类事件。
	EventNotice = "NOTICE"

	// EventInteraction 交互事件（按钮回调、斜杠命令等）。
	EventInteraction = "INTERACTION"

	// EventReaction 消息表情回应。
	EventReaction = "REACTION"

	// EventMemberJoin 成员加入群组/服务器。
	EventMemberJoin = "MEMBER_JOIN"

	// EventMemberLeave 成员离开群组/服务器。
	EventMemberLeave = "MEMBER_LEAVE"

	// EventMessageUpdate 消息编辑。
	EventMessageUpdate = "MESSAGE_UPDATE"

	// EventMessageDelete 消息撤回/删除。
	EventMessageDelete = "MESSAGE_DELETE"
)
