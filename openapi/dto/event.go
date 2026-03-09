package dto

// EventType is the type of the event
type EventType string

const (
	Ready                EventType = "READY"
	Resumed              EventType = "RESUMED"
	C2CMessageCreate     EventType = "C2C_MESSAGE_CREATE"
	GroupAtMessageCreate EventType = "GROUP_AT_MESSAGE_CREATE"
	GroupAddRobot        EventType = "GROUP_ADD_ROBOT"
	GroupDelRobot        EventType = "GROUP_DEL_ROBOT"
	GroupMsgReject       EventType = "GROUP_MSG_REJECT"
	GroupMsgReceive      EventType = "GROUP_MSG_RECEIVE"
	FriendAdd            EventType = "FRIEND_ADD"
	FriendDel            EventType = "FRIEND_DEL"
	C2CMsgReject         EventType = "C2C_MSG_REJECT"
	C2CMsgReceive        EventType = "C2C_MSG_RECEIVE"

	// Channel (频道) 事件类型
	// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/
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
	Type     string `json:"content_type,omitempty"`
	FileName string `json:"filename,omitempty"`
	Height   int    `json:"height,omitempty"`
	Width    int    `json:"width,omitempty"`
	Size     int    `json:"size,omitempty"`
	URL      string `json:"url,omitempty"`
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

//// --- 频道（Guild / Channel）事件 ---
//
//// GuildEvent 频道基础事件（机器人加入/退出频道，或频道信息变更）
////
//// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/
//type GuildEvent struct {
//	ID          string `json:"id,omitempty"`           // 频道 ID
//	Name        string `json:"name,omitempty"`         // 频道名称
//	Description string `json:"description,omitempty"`  // 频道描述
//	OwnerID     string `json:"owner_id,omitempty"`     // 创建人 ID
//	JoinedAt    string `json:"joined_at,omitempty"`    // 机器人加入时间（RFC3339）
//	MemberCount int    `json:"member_count,omitempty"` // 成员数量
//}
//
//// GuildCreateEvent 机器人加入频道事件
//type GuildCreateEvent struct {
//	GuildEvent
//}
//
//// GuildUpdateEvent 频道信息变更事件
//type GuildUpdateEvent struct {
//	GuildEvent
//}
//
//// GuildDeleteEvent 机器人退出频道事件
//type GuildDeleteEvent struct {
//	GuildEvent
//}
//
//// GuildMemberEvent 频道成员事件基础结构
//type GuildMemberEvent struct {
//	GuildID  string `json:"guild_id,omitempty"`   // 频道 ID
//	UserID   string `json:"user_id,omitempty"`    // 用户 ID
//	Nick     string `json:"nick,omitempty"`       // 用户在频道内昵称
//	JoinedAt string `json:"joined_at,omitempty"`  // 加入时间（RFC3339）
//	OpUserID string `json:"op_user_id,omitempty"` // 操作人 ID
//}
//
//// GuildMemberAddEvent 频道成员加入事件
//type GuildMemberAddEvent struct {
//	GuildMemberEvent
//}
//
//// GuildMemberUpdateEvent 频道成员更新事件
//type GuildMemberUpdateEvent struct {
//	GuildMemberEvent
//}
//
//// GuildMemberRemoveEvent 频道成员移除事件
//type GuildMemberRemoveEvent struct {
//	GuildMemberEvent
//}
//
//// ChannelEvent 子频道事件基础结构
//type ChannelEvent struct {
//	ID       string `json:"id,omitempty"`         // 子频道 ID
//	GuildID  string `json:"guild_id,omitempty"`   // 所属频道 ID
//	Name     string `json:"name,omitempty"`       // 子频道名称
//	Type     int    `json:"type,omitempty"`       // 子频道类型
//	SubType  int    `json:"sub_type,omitempty"`   // 子频道子类型
//	Position int    `json:"position,omitempty"`   // 排序权重
//	OpUserID string `json:"op_user_id,omitempty"` // 操作人 ID
//}
//
//// ChannelCreateEvent 子频道创建事件
//type ChannelCreateEvent struct {
//	ChannelEvent
//}
//
//// ChannelUpdateEvent 子频道更新事件
//type ChannelUpdateEvent struct {
//	ChannelEvent
//}
//
//// ChannelDeleteEvent 子频道删除事件
//type ChannelDeleteEvent struct {
//	ChannelEvent
//}
//
//// ChannelMessageCreateEvent 频道内 @机器人 或普通消息事件
////
//// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/event.html
//type ChannelMessageCreateEvent struct {
//	MessageCreateEvent
//	GuildID   string `json:"guild_id,omitempty"`   // 频道 ID
//	ChannelID string `json:"channel_id,omitempty"` // 子频道 ID
//}
//
//// DirectMessageCreateEvent 频道私信消息事件
////
//// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/event.html
//type DirectMessageCreateEvent struct {
//	MessageCreateEvent
//	GuildID   string `json:"guild_id,omitempty"`   // 私信会话频道 ID
//	ChannelID string `json:"channel_id,omitempty"` // 私信子频道 ID
//}
