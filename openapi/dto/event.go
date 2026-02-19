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

// TODO: Add channel event
