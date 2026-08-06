// Package platform 定义平台无关的消息与事件抽象层。
//
// 设计目标：
//   - 将框架核心（engine、context）与具体平台（QQ官方、Discord、Telegram 等）解耦
//   - 现有 QQ 适配器通过 platform/qq 包实现本接口，向后兼容
//   - 新平台只需实现 Adapter + Event 接口，无需改动核心引擎
//
// 层次结构：
//
//	┌──────────────┐
//	│   Bot/Engine │  使用 platform.Event / platform.Adapter
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
	// EventKindGroupMessage 群组消息（QQ 群、Discord 频道等）
	EventKindGroupMessage EventKind = "GROUP_MESSAGE"
	// EventKindGuildMessage 频道/服务器消息（QQ频道、Discord 服务器等）
	EventKindGuildMessage EventKind = "GUILD_MESSAGE"
	// EventKindNotice 通知类事件（通用兜底，平台无法精确归类时使用）。
	//
	// 优先使用下方的细粒度 Kind（BotAdded / FriendAdded 等）；
	// 仅当确实无法归类时才使用此值，配合 platform.RawType(event) 做进一步区分。
	EventKindNotice EventKind = "NOTICE"
	// EventKindRequest 请求类事件（加好友请求、加群请求等）
	EventKindRequest EventKind = "REQUEST"
	// EventKindSystem 系统事件（Ready、Resumed 等）
	EventKindSystem EventKind = "SYSTEM"
	// EventKindInteraction 交互事件（按钮回调、斜杠命令、下拉菜单等）
	//
	// Discord Interaction、QQ 机器人 v2 按钮回调、Telegram 内联键盘回调。
	EventKindInteraction EventKind = "INTERACTION"
	// EventKindReaction 消息表情回应（添加或移除）
	//
	// Discord 表情回应、Telegram 表情回应、QQ 表情回应。
	EventKindReaction EventKind = "REACTION"
	// EventKindMemberJoin 普通成员加入群组/服务器事件（非机器人自身）。
	//
	// 机器人自身被加入群组/频道请使用 [EventKindBotAdded]。
	EventKindMemberJoin EventKind = "MEMBER_JOIN"
	// EventKindMemberLeave 普通成员离开/被踢出群组/服务器事件（非机器人自身）。
	//
	// 机器人自身被移出群组/频道请使用 [EventKindBotRemoved]。
	EventKindMemberLeave EventKind = "MEMBER_LEAVE"
	// EventKindMemberUpdate 成员信息变更（昵称、角色、权限等）。
	//
	// QQ 频道 GuildMemberUpdate、Discord guild_member_update 等。
	EventKindMemberUpdate EventKind = "MEMBER_UPDATE"
	// EventKindMessageUpdate 消息被编辑
	//
	// Discord 消息编辑、Telegram 消息编辑。
	EventKindMessageUpdate EventKind = "MESSAGE_UPDATE"
	// EventKindMessageDelete 消息被撤回/删除
	EventKindMessageDelete EventKind = "MESSAGE_DELETE"

	// ── 机器人自身生命周期 ─────────────────────────────────────────────────

	// EventKindBotAdded 机器人自身被加入某个群组/频道/服务器。
	//
	// QQ: GROUP_ADD_ROBOT（被加入群）、GUILD_CREATE（被加入频道）
	// Discord: guild_create（机器人加入新服务器）
	EventKindBotAdded EventKind = "BOT_ADDED"

	// EventKindBotRemoved 机器人自身被移出群组/频道/服务器。
	//
	// QQ: GROUP_DEL_ROBOT（被移出群）、GUILD_DELETE（被移出频道）
	// Discord: guild_delete（机器人离开服务器）
	EventKindBotRemoved EventKind = "BOT_REMOVED"

	// ── 好友/关注者 ────────────────────────────────────────────────────────

	// EventKindFriendAdded 新好友/关注者。
	//
	// QQ: FRIEND_ADD（C2C 场景用户添加机器人为好友/关注）
	EventKindFriendAdded EventKind = "FRIEND_ADDED"

	// EventKindFriendRemoved 好友/关注者移除。
	//
	// QQ: FRIEND_DEL（C2C 场景用户删除机器人好友/取消关注）
	EventKindFriendRemoved EventKind = "FRIEND_REMOVED"

	// ── 消息权限变更 ───────────────────────────────────────────────────────

	// EventKindMsgPermissionChange 消息权限变更（消息下发开启/关闭）。
	//
	// QQ: GROUP_MSG_REJECT（群关闭机器人消息）、GROUP_MSG_RECEIVE（群开启机器人消息）、
	//     C2C_MSG_REJECT（C2C 关闭机器人消息）、  C2C_MSG_RECEIVE（C2C 开启机器人消息）
	EventKindMsgPermissionChange EventKind = "MSG_PERMISSION_CHANGE"

	// ── 频道/子频道 ────────────────────────────────────────────────────────

	// EventKindChannelChange 子频道（channel）创建、更新或删除。
	//
	// QQ: CHANNEL_CREATE / CHANNEL_UPDATE / CHANNEL_DELETE
	// Discord: channel_create / channel_update / channel_delete
	EventKindChannelChange EventKind = "CHANNEL_CHANGE"

	// EventKindGuildChange 服务器/频道（guild）信息更新（非加入/离开）。
	//
	// QQ: GUILD_UPDATE；Discord: guild_update
	EventKindGuildChange EventKind = "GUILD_CHANGE"

	// ── 消息审核 ───────────────────────────────────────────────────────────

	// EventKindMessageAudit 消息审核结果通知。
	//
	// QQ: MESSAGE_AUDIT（主动消息推送后的审核结果回调）
	EventKindMessageAudit EventKind = "MESSAGE_AUDIT"
)

// GroupRole 发送者在当前群/频道中的角色等级。
//
// 仅在群组消息中有意义；私聊场景值为 GroupRoleUnknown。
// 各平台填充能力不同：Discord 通过 Member.Permissions 推断；
// QQ 群消息暂不在事件 payload 中提供（需额外 API 调用）。
type GroupRole int

const (
	// GroupRoleUnknown 未知角色（平台未提供信息，或私聊场景）
	GroupRoleUnknown GroupRole = 0
	// GroupRoleMember 普通成员
	GroupRoleMember GroupRole = 1
	// GroupRoleAdmin 群管理员（具有管理群/频道权限的角色）
	GroupRoleAdmin GroupRole = 2
	// GroupRoleOwner 群主/服务器拥有者，或具有最高管理权限（Administrator）的成员
	GroupRoleOwner GroupRole = 3
)

// UserInfo 代表消息发送者/用户的基本信息。
//
// 各平台填充能力不同，未知字段返回空字符串、false 或零值。
type UserInfo struct {
	// ID 平台内唯一用户标识（QQ openID、Telegram userID 等）
	ID string
	// DisplayName 用户显示名（昵称/用户名）
	DisplayName string
	// GroupRole 发送者在当前群/频道中的角色等级。
	// 私聊场景或平台未提供时为 GroupRoleUnknown。
	GroupRole GroupRole
	// IsBot 是否为机器人账号
	IsBot bool
	// IsSelf 此用户信息是否指向机器人自身（如 @ 列表中的机器人自身）。
	// 主要用于 MentionsEvent，方便插件快速判断机器人是否被 @。
	IsSelf bool
}

// ChatInfo 代表消息所在会话的基本信息。
type ChatInfo struct {
	// ID 会话/群组/频道唯一标识
	// 私聊：用户 ID；群组：群 ID；频道/话题：channel_id
	ID string
	// ParentID 父容器唯一标识（服务器/频道层级时使用）。
	//
	// 频道消息：guild_id / 服务器 ID
	// Discord: guild_id；QQ 频道: guild_id
	// 私聊和普通群组为空字符串。
	ParentID string
	// Name 会话名称（可选，部分平台不提供）
	Name string
	// IsGroup 是否为群组/频道消息（false = 私聊）
	IsGroup bool
	// IsDM 是否为私信（Direct Message）会话。
	//
	// 与 IsGroup=true、ParentID 非空同时成立时，
	// 表示这是一条频道私信（如 QQ DIRECT_MESSAGE_CREATE），
	// 发送回复时应使用 DM 专属接口而非普通频道消息接口。
	IsDM bool

	// Tokens 平台专属授权令牌，用于平台内部路由或被动回复授权。
	//
	// 各平台适配器在解析事件时写入，平台 Sender 在发送时读取。
	// 框架层 handler 通常无需直接访问此字段。
	//
	// 已知 token 键（见各平台 extra.go 中的常量定义）：
	//   - QQ: TokenMsgID ("msg_id")、TokenEventID ("event_id")
	//
	// 读取 nil map 是安全的（返回空字符串），写入前须先初始化。
	Tokens map[string]string
}

// EventBody 是事件的消息载荷接口。
//
// 包含 Content（文本内容）和 Attachments（附件列表）。
// 只关心消息内容的插件可依赖此接口，无需依赖完整的 Event。
type EventBody interface {
	// Content 返回消息文本内容（纯文本，不含平台特定格式）
	Content() string

	// Attachments 返回消息中携带的附件列表。
	//
	// 平台不支持附件或消息无附件时返回 nil。
	Attachments() []Attachment
}

// EventIdentity 是事件路由和去重标识接口。
//
// 包含 Platform（路由到对应适配器）、Kind（事件分类路由）、
// ID（去重/追踪）。中间件（dedup、retry、deadletter）通常只需此接口。
type EventIdentity interface {
	// Platform 返回平台标识符（如 "qq"、"discord"、"telegram"）
	Platform() string

	// Kind 返回平台无关的事件类别
	Kind() EventKind

	// ID 返回平台级别的唯一事件标识符。
	//
	// 用途：去重、追踪、死信队列等需要唯一标识的场景。
	// 平台不提供时返回空字符串；调用方应对空字符串做兼容处理。
	ID() string
}

// Event 是平台无关的事件抽象接口（最小必要集合）。
//
// 组合 EventIdentity（路由/去重）和 EventBody（消息载荷），
// 外加 Sender、Chat、Timestamp 三个独立方法。
//
// 各平台适配器将原始 payload 包装为 Event 实现，
// 框架核心只依赖此接口，不直接引用任何平台特定结构体。
//
// 调用方可选择性收窄依赖：
//   - 仅需去重追踪：依赖 EventIdentity（如 dedup 中间件）
//   - 仅需消息内容：依赖 EventBody（如内容分析插件）
//   - 完整事件处理：依赖 Event
//
// 平台特定或可选功能通过独立接口扩展：
//   - [RawEvent]：访问平台原始类型字符串和 payload
//   - [EditableEvent]：判断消息是否为编辑版本
//   - [ReplyEvent]：获取被回复消息的 ID（回复链/消息线程）
//
// 使用类型断言检测可选能力：
//
//	if re, ok := event.(platform.ReplyEvent); ok {
//	    replyID := re.ReplyToID()
//	}
//
// 或使用包级帮助函数（优先推荐）：
//
//	rawType := platform.RawType(event)   // 若不支持则返回 ""
//	replyID := platform.GetReplyToID(event) // 若不支持则返回 ""
type Event interface {
	EventIdentity
	EventBody

	// Segments 返回有序消息段（唯一真相源）。
	//
	// Content()/Attachments() 是便捷派生视图：
	//   - Content()      = SegmentsContent(Segments())
	//   - Attachments()  = SegmentsAttachments(Segments())
	Segments() []Segment

	// Sender 返回消息发送者信息
	Sender() UserInfo

	// Chat 返回消息所在会话信息
	Chat() ChatInfo

	// Timestamp 返回事件时间戳（尽力而为，平台不提供时返回零值）
	Timestamp() time.Time
}

// ────────────────────────────────────────────────────────────────────────────
// 可选扩展接口
// ────────────────────────────────────────────────────────────────────────────

// RawEvent 是平台特定数据的可选访问接口。
//
// 适配器在需要暴露底层 payload 或平台原生类型字符串时实现此接口。
// 框架核心代码通过 [RawType] / [RawPayload] 帮助函数安全访问，无需直接断言。
type RawEvent interface {
	// RawType 返回平台原始事件类型字符串（如 QQ 的 "C2C_MESSAGE_CREATE"）
	RawType() string

	// RawPayload 返回原始平台 payload（类型断言后可访问平台特定字段）
	//
	// 示例（QQ 平台）:
	//   if payload, ok := e.RawPayload().(*dto.Payload); ok { ... }
	RawPayload() any
}

// EditableEvent 是消息编辑感知的可选接口（D3）。
//
// 支持消息编辑事件的平台（Discord、Telegram）实现此接口。
// 使用示例：
//
//	if ee, ok := event.(platform.EditableEvent); ok && ee.IsEdited() {
//	    log.Printf("消息已编辑，原始时间戳: %v", ee.OriginalTimestamp())
//	}
type EditableEvent interface {
	// IsEdited 返回此事件是否为消息编辑事件
	IsEdited() bool
	// OriginalTimestamp 返回原始消息的发送时间戳（零值表示不可用）
	OriginalTimestamp() time.Time
}

// ReplyEvent 是消息回复链感知的可选接口（N7）。
//
// 支持消息回复（线程）的平台实现此接口。
// 框架通过 [GetReplyToID] 帮助函数安全访问，无需直接断言。
//
// 使用示例：
//
//	if id := platform.GetReplyToID(event); id != "" {
//	    // 此消息是对 id 的回复
//	}
type ReplyEvent interface {
	// ReplyToID 返回被回复消息的平台原生 ID。
	// 若此消息不是回复，或平台不提供此信息，返回空字符串。
	ReplyToID() string
}

// MentionsEvent 是 @ 用户列表感知的可选接口。
//
// 消息中携带 @ 用户列表（QQ group_at_message、Discord mentions、
// Telegram entities 中的 mention）时，适配器实现此接口。
// 框架通过 [GetMentions] 帮助函数安全访问，无需直接断言。
//
// 使用示例：
//
//	if mentions := platform.GetMentions(event); len(mentions) > 0 {
//	    for _, u := range mentions {
//	        log.Printf("@ 了用户 %s (%s)", u.DisplayName, u.ID)
//	    }
//	}
type MentionsEvent interface {
	// Mentions 返回消息中 @ 的用户列表（不含机器人自身）。
	// 无 @ 用户时返回 nil。
	Mentions() []UserInfo
}

// ────────────────────────────────────────────────────────────────────────────
// 可选接口帮助函数
// ────────────────────────────────────────────────────────────────────────────

// RawType 安全获取平台原始事件类型字符串。
//
// 若事件未实现 [RawEvent]，返回空字符串。
func RawType(e Event) string {
	if re, ok := e.(RawEvent); ok {
		return re.RawType()
	}
	return ""
}

// RawPayload 安全获取平台原始 payload。
//
// 若事件未实现 [RawEvent]，返回 nil。
func RawPayload(e Event) any {
	if re, ok := e.(RawEvent); ok {
		return re.RawPayload()
	}
	return nil
}

// GetReplyToID 安全获取被回复消息的 ID。
//
// 派生顺序（§3.2 reply 单一真相源）：段内首个 SegmentReply → 接口断言兜底。
// 平台实现其 ReplyToID() 时应直接委托段查找，杜绝双写。
// 若事件无 reply 段且未实现 [ReplyEvent]，返回空字符串。
func GetReplyToID(e Event) string {
	for _, s := range e.Segments() {
		if s.Type == SegmentReply && s.ReplyToID != "" {
			return s.ReplyToID
		}
	}
	if re, ok := e.(ReplyEvent); ok {
		return re.ReplyToID()
	}
	return ""
}

// IsEdited 安全判断消息是否为编辑版本。
//
// 若事件未实现 [EditableEvent]，返回 false。
func IsEdited(e Event) bool {
	if ee, ok := e.(EditableEvent); ok {
		return ee.IsEdited()
	}
	return false
}

// GetMentions 安全获取消息中 @ 的用户列表。
//
// 派生顺序：接口断言优先（平台可实现更丰富的 UserInfo，如 qq 的 IsBot）、
// 段兜底（SegmentsMentions 派生，botID 缺失时以段内 Extra[SegmentExtraIsSelf] 覆盖为准）。
// 若事件无 @ 用户，返回 nil。
func GetMentions(e Event) []UserInfo {
	if me, ok := e.(MentionsEvent); ok {
		return me.Mentions()
	}
	return SegmentsMentions(e.Segments(), "")
}
