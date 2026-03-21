package dto

// EventType is the type of the event.
// It is a type alias for string so that engine.EventType and dto.EventType
// are interchangeable without explicit conversion.
type EventType = string

const (
	Ready                 EventType = "READY"
	Resumed               EventType = "RESUMED"
	C2CMessageCreate      EventType = "C2C_MESSAGE_CREATE"
	GroupAtMessageCreate  EventType = "GROUP_AT_MESSAGE_CREATE"
	GroupAddRobot         EventType = "GROUP_ADD_ROBOT"
	GroupDelRobot         EventType = "GROUP_DEL_ROBOT"
	GroupMsgReject        EventType = "GROUP_MSG_REJECT"
	GroupMsgReceive       EventType = "GROUP_MSG_RECEIVE"
	FriendAdd             EventType = "FRIEND_ADD"
	FriendDel             EventType = "FRIEND_DEL"
	C2CMsgReject          EventType = "C2C_MSG_REJECT"
	C2CMsgReceive         EventType = "C2C_MSG_RECEIVE"
	InteractionCreate     EventType = "INTERACTION_CREATE"      // 互动事件（按钮回调等）
	MessageReactionAdd    EventType = "MESSAGE_REACTION_ADD"    // 用户发表表情表态
	MessageReactionRemove EventType = "MESSAGE_REACTION_REMOVE" // 用户取消表情表态

	ChannelCreate       EventType = "CHANNEL_CREATE"        // 子频道创建
	ChannelUpdate       EventType = "CHANNEL_UPDATE"        // 子频道更新
	ChannelDelete       EventType = "CHANNEL_DELETE"        // 子频道删除
	GuildCreate         EventType = "GUILD_CREATE"          // 频道创建（机器人加入）
	GuildUpdate         EventType = "GUILD_UPDATE"          // 频道信息变更
	GuildDelete         EventType = "GUILD_DELETE"          // 频道删除（机器人退出）
	GuildMemberAdd      EventType = "GUILD_MEMBER_ADD"      // 频道成员加入
	GuildMemberUpdate   EventType = "GUILD_MEMBER_UPDATE"   // 频道成员更新
	GuildMemberRemove   EventType = "GUILD_MEMBER_REMOVE"   // 频道成员移除
	AtMessageCreate     EventType = "AT_MESSAGE_CREATE"     // 频道内 @机器人 消息
	MessageCreate       EventType = "MESSAGE_CREATE"        // 频道内消息（需申请权限）
	MessageDeleteEvent  EventType = "MESSAGE_DELETE"        // 频道消息删除（撤回）
	DirectMessageCreate EventType = "DIRECT_MESSAGE_CREATE" // 频道私信消息
)

// EventID is the ID of the event
type EventID string

// MessageCreateEvent is a struct that holds the event data
type MessageCreateEvent struct {
	ID          EventID      `json:"id,omitempty"`
	Content     string       `json:"content,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"` //RFC3339 format
	Attachments []Attachment `json:"attachments,omitempty"`
	Author      Author       `json:"author"`
}

// Attachment represents an attachment in the event
type Attachment struct {
	Type         string `json:"content_type,omitempty"`
	FileName     string `json:"filename,omitempty"`
	Height       int    `json:"height,omitempty"`
	Width        int    `json:"width,omitempty"`
	Size         int    `json:"size,omitempty"`
	URL          string `json:"url,omitempty"`
	VoiceWavURL  string `json:"voice_wav_url,omitempty"`  // 语音文件链接（wav 格式）
	AsrReferText string `json:"asr_refer_text,omitempty"` // 语音 ASR 参考结果
}

// Author represents the author of the event
type Author struct {
	ID           string `json:"id,omitempty"`
	MemberOpenID string `json:"member_openid,omitempty"` // OpenID of the member
	UnionOpenID  string `json:"union_openid,omitempty"`
	UserOpenID   string `json:"user_openid,omitempty"`
}

// C2CMessageCreateEvent represents a C2C message create event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/event.html#%E5%8D%95%E8%81%8A%E6%B6%88%E6%81%AF
type C2CMessageCreateEvent struct {
	MessageCreateEvent
}

// GroupAtMessageCreateEvent represents a group at message create event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/event.html#%E7%BE%A4%E8%81%8A-%E6%9C%BA%E5%99%A8%E4%BA%BA
type GroupAtMessageCreateEvent struct {
	MessageCreateEvent
	GroupOpenID string `json:"group_openid,omitempty"`
}

// GroupOpRobotEvent represents a group operation robot event
type GroupOpRobotEvent struct {
	Timestamp      int    `json:"timestamp,omitempty"`
	GroupOpenID    string `json:"group_openid,omitempty"`
	OpMemberOpenID string `json:"op_member_openid,omitempty"` // Operator member who added the robot
}

// GroupAddRobotEvent represents a group add robot event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/group/manage/event.html#%E6%9C%BA%E5%99%A8%E4%BA%BA%E5%8A%A0%E5%85%A5%E7%BE%A4%E8%81%8A
type GroupAddRobotEvent struct {
	GroupOpRobotEvent
}

// GroupDelRobotEvent represents a group remove robot event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/group/manage/event.html#%E6%9C%BA%E5%99%A8%E4%BA%BA%E9%80%80%E5%87%BA%E7%BE%A4%E8%81%8A
type GroupDelRobotEvent struct {
	GroupOpRobotEvent
}

// GroupMsgRejectEvent represents a group message reject event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/group/manage/event.html#%E7%BE%A4%E8%81%8A%E6%8B%92%E7%BB%9D%E6%9C%BA%E5%99%A8%E4%BA%BA%E4%B8%BB%E5%8A%A8%E6%B6%88%E6%81%AF
type GroupMsgRejectEvent struct {
	GroupOpRobotEvent
}

// GroupMsgReceiveEvent represents a group message receive event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/group/manage/event.html#%E7%BE%A4%E8%81%8A%E6%8E%A5%E5%8F%97%E6%9C%BA%E5%99%A8%E4%BA%BA%E4%B8%BB%E5%8A%A8%E6%B6%88%E6%81%AF
type GroupMsgReceiveEvent struct {
	GroupOpRobotEvent
}

// UserOpRobotEvent represents a user operation robot event
type UserOpRobotEvent struct {
	Timestamp int    `json:"timestamp,omitempty"`
	OpenID    string `json:"openid,omitempty"` // user who added the robot
}

// FriendAddEvent represents a friend add event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/user/manage/event.html#%E7%94%A8%E6%88%B7%E6%B7%BB%E5%8A%A0%E6%9C%BA%E5%99%A8%E4%BA%BA
type FriendAddEvent struct {
	UserOpRobotEvent
	// Scene 加好友场景值：1000=默认，1001~1004=搜索/群/空间，2001~2004=分享链接
	Scene int `json:"scene,omitempty"`
	// SceneParam 开发者自定义的回调数据（机器人链接中的 callback_data）
	SceneParam string `json:"scene_param,omitempty"`
}

// FriendDelEvent represents a friend delete event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/user/manage/event.html#%E7%94%A8%E6%88%B7%E5%88%A0%E9%99%A4%E6%9C%BA%E5%99%A8%E4%BA%BA
type FriendDelEvent struct {
	UserOpRobotEvent
}

// C2CMsgRejectEvent represents a C2C message reject event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/user/manage/event.html#%E6%8B%92%E7%BB%9D%E6%9C%BA%E5%99%A8%E4%BA%BA%E4%B8%BB%E5%8A%A8%E6%B6%88%E6%81%AF
type C2CMsgRejectEvent struct {
	UserOpRobotEvent
}

// C2CMsgReceiveEvent represents a C2C message receive event
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/user/manage/event.html#%E5%85%81%E8%AE%B8%E6%9C%BA%E5%99%A8%E4%BA%BA%E4%B8%BB%E5%8A%A8%E6%B6%88%E6%81%AF
type C2CMsgReceiveEvent struct {
	UserOpRobotEvent
}

// ────────────────────────────────────────────────────────────────────────────
// 互动事件（INTERACTION_CREATE）— 按钮回调 / 单聊快捷菜单
//
// intents: 1<<26
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/msg-btn.html#%E4%BA%8B%E4%BB%B6
// ────────────────────────────────────────────────────────────────────────────

// InteractionCreateEvent INTERACTION_CREATE 事件载体。
//
// 用户点击消息按钮或单聊快捷菜单后，平台推送此事件。
// 收到后必须调用 openapi.RespondInteraction 回应，否则客户端会一直 loading 至超时。
type InteractionCreateEvent struct {
	// ID 平台方事件 ID，可用于被动消息发送
	ID string `json:"id"`
	// Type 11=消息按钮，12=单聊快捷菜单
	Type int `json:"type"`
	// Scene 事件场景：c2c、group、guild
	Scene string `json:"scene"`
	// ChatType 0=频道，1=群聊，2=单聊
	ChatType int `json:"chat_type"`
	// Timestamp 触发时间（RFC3339）
	Timestamp string `json:"timestamp"`
	// GuildID 频道 openid（仅频道场景）
	GuildID string `json:"guild_id,omitempty"`
	// ChannelID 文字子频道 openid（仅频道场景）
	ChannelID string `json:"channel_id,omitempty"`
	// UserOpenID 触发用户 openid（仅单聊场景）
	UserOpenID string `json:"user_openid,omitempty"`
	// GroupOpenID 群 openid（仅群聊场景）
	GroupOpenID string `json:"group_openid,omitempty"`
	// GroupMemberOpenID 触发用户的群成员 openid（仅群聊场景）
	GroupMemberOpenID string `json:"group_member_openid,omitempty"`
	// Data 互动数据
	Data *InteractionData `json:"data,omitempty"`
	// Version 默认 1
	Version int `json:"version"`
}

// InteractionData 互动事件数据。
type InteractionData struct {
	// Resolved 解析后的操作详情
	Resolved *InteractionResolved `json:"resolved,omitempty"`
	// Type 11=消息按钮，12=单聊快捷菜单
	Type int `json:"type"`
}

// InteractionResolved 互动事件解析详情。
type InteractionResolved struct {
	// ButtonData 被点击按钮的 action.data 值
	ButtonData string `json:"button_data,omitempty"`
	// ButtonID 被点击按钮的 id 值
	ButtonID string `json:"button_id,omitempty"`
	// UserID 操作用户 userid（仅频道场景）
	UserID string `json:"user_id,omitempty"`
	// FeatureID 自定义菜单的按钮 id（仅自定义菜单，后台设置）
	FeatureID string `json:"feature_id,omitempty"`
	// MessageID 被操作消息 id（仅频道场景）
	MessageID string `json:"message_id,omitempty"`
}

// MessageReactionEvent 表情表态事件载体（MESSAGE_REACTION_ADD / MESSAGE_REACTION_REMOVE）。
//
// intents: GUILD_MESSAGE_REACTIONS = 1<<10
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/emoji.html#事件
type MessageReactionEvent struct {
	// UserID 发表表情表态的用户 ID
	UserID string `json:"user_id"`
	// ChannelID 所在文字子频道 ID
	ChannelID string `json:"channel_id"`
	// GuildID 所在频道 ID
	GuildID string `json:"guild_id"`
	// Emoji 表情信息
	Emoji struct {
		// ID 表情 ID
		ID string `json:"id"`
		// Type 表情类型（1=系统表情，2=emoji）
		Type int `json:"type"`
	} `json:"emoji"`
	// Target 被表态的消息/对象
	Target struct {
		// ID 被表态对象 ID（如消息 ID）
		ID string `json:"id"`
		// Type 被表态对象类型（0=默认消息）
		Type int `json:"type"`
	} `json:"target"`
}

//// --- 频道（Guild / Channel）事件 ---
//
//// GuildEvent 频道基础事件（机器人加入/退出频道，或频道信息变更）
////
//// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/
//type GuildEvent struct {

// ────────────────────────────────────────────────────────────────────────────
// 频道（Guild / Channel）事件
// ────────────────────────────────────────────────────────────────────────────

// GuildEvent 频道基础事件（机器人加入/退出频道，或频道信息变更）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/manage/event/guild.html
type GuildEvent struct {
	ID          string `json:"id,omitempty"`           // 频道 ID
	Name        string `json:"name,omitempty"`         // 频道名称
	Description string `json:"description,omitempty"`  // 频道描述
	OwnerID     string `json:"owner_id,omitempty"`     // 创建人 ID
	JoinedAt    string `json:"joined_at,omitempty"`    // 机器人加入时间（RFC3339）
	MemberCount int    `json:"member_count,omitempty"` // 成员数量
}

// GuildCreateEvent 机器人加入频道事件（intents: GUILDS = 1<<0）。
type GuildCreateEvent struct {
	GuildEvent
}

// GuildUpdateEvent 频道信息变更事件。
type GuildUpdateEvent struct {
	GuildEvent
}

// GuildDeleteEvent 机器人退出频道事件。
type GuildDeleteEvent struct {
	GuildEvent
}

// GuildMemberEvent 频道成员事件基础结构。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/manage/event/guild.html
type GuildMemberEvent struct {
	GuildID  string `json:"guild_id,omitempty"`   // 频道 ID
	Nick     string `json:"nick,omitempty"`       // 用户在频道内昵称
	JoinedAt string `json:"joined_at,omitempty"`  // 加入时间（RFC3339）
	OpUserID string `json:"op_user_id,omitempty"` // 操作人 ID
	User     *struct {
		ID       string `json:"id,omitempty"`
		Username string `json:"username,omitempty"`
		Avatar   string `json:"avatar,omitempty"`
		Bot      bool   `json:"bot,omitempty"`
	} `json:"user,omitempty"`
}

// GuildMemberAddEvent 频道成员加入事件。
type GuildMemberAddEvent struct {
	GuildMemberEvent
}

// GuildMemberUpdateEvent 频道成员更新事件。
type GuildMemberUpdateEvent struct {
	GuildMemberEvent
}

// GuildMemberRemoveEvent 频道成员移除事件。
type GuildMemberRemoveEvent struct {
	GuildMemberEvent
}

// ChannelEvent 子频道事件基础结构。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/manage/event/channel.html
type ChannelEvent struct {
	ID       string `json:"id,omitempty"`         // 子频道 ID
	GuildID  string `json:"guild_id,omitempty"`   // 所属频道 ID
	Name     string `json:"name,omitempty"`       // 子频道名称
	Type     int    `json:"type,omitempty"`       // 子频道类型
	SubType  int    `json:"sub_type,omitempty"`   // 子频道子类型
	Position int    `json:"position,omitempty"`   // 排序权重
	OpUserID string `json:"op_user_id,omitempty"` // 操作人 ID
}

// ChannelCreateEvent 子频道创建事件。
type ChannelCreateEvent struct {
	ChannelEvent
}

// ChannelUpdateEvent 子频道更新事件。
type ChannelUpdateEvent struct {
	ChannelEvent
}

// ChannelDeleteEvent 子频道删除事件。
type ChannelDeleteEvent struct {
	ChannelEvent
}

// MessageDeleteEventData 频道消息撤回事件载体。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/event.html
type MessageDeleteEventData struct {
	Message struct {
		ID        string `json:"id,omitempty"`
		ChannelID string `json:"channel_id,omitempty"`
		GuildID   string `json:"guild_id,omitempty"`
	} `json:"message"`
	OpUser struct {
		ID string `json:"id,omitempty"`
	} `json:"op_user"`
}
