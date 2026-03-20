// Package qq 是 QQ 官方机器人平台的 platform.PlatformAdapter 实现。
//
// 本包将 QQ 官方数据结构（dto.Payload、dto.C2CMessageCreateEvent 等）
// 包装为 platform.Event，使框架核心不再直接依赖 QQ SDK 类型。
package qq

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

const PlatformID = "qq"

// qqEvent 将 QQ 的 *dto.Payload 包装为 platform.Event
type qqEvent struct {
	payload   *dto.Payload
	kind      platform.EventKind
	sender    platform.UserInfo
	chat      platform.ChatInfo
	content   string
	timestamp time.Time
}

// NewEvent 从 QQ payload 创建 platform.Event
//
// 自动解析高频事件类型（C2C、GroupAt），其他类型懒解析。
func NewEvent(payload *dto.Payload) platform.Event {
	e := &qqEvent{
		payload: payload,
	}
	e.populate()
	return e
}

// populate 从 payload 中提取平台无关字段
func (e *qqEvent) populate() {
	if e.payload == nil {
		e.kind = platform.EventKindUnknown
		return
	}

	switch e.payload.Type {
	case dto.C2CMessageCreate:
		e.kind = platform.EventKindPrivateMessage
		e.populateC2C()

	case dto.GroupAtMessageCreate:
		e.kind = platform.EventKindGroupMessage
		e.populateGroupAt()

	case dto.Ready, dto.Resumed:
		e.kind = platform.EventKindSystem

	case dto.GroupAddRobot, dto.GroupDelRobot, dto.GroupMsgReject, dto.GroupMsgReceive:
		e.kind = platform.EventKindNotice
		e.populateNoticeGroup()

	case dto.FriendAdd, dto.FriendDel, dto.C2CMsgReject, dto.C2CMsgReceive:
		e.kind = platform.EventKindNotice
		e.populateNoticeUser()

	case dto.AtMessageCreate, dto.MessageCreate, dto.DirectMessageCreate:
		e.kind = platform.EventKindGuildMessage
		e.populateGuildMessage()

	default:
		e.kind = platform.EventKindUnknown
	}
}

func (e *qqEvent) populateC2C() {
	if e.payload.Detail == nil {
		return
	}
	d := e.payload.Detail
	e.content = gjson.GetBytes(d, "content").String()

	userOpenID := gjson.GetBytes(d, "author.user_openid").String()
	e.sender = platform.UserInfo{
		ID:          userOpenID,
		DisplayName: gjson.GetBytes(d, "author.id").String(),
	}
	e.chat = platform.ChatInfo{
		ID:      userOpenID,
		IsGroup: false,
	}

	if ts := gjson.GetBytes(d, "timestamp").String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

func (e *qqEvent) populateGroupAt() {
	if e.payload.Detail == nil {
		return
	}
	d := e.payload.Detail
	e.content = gjson.GetBytes(d, "content").String()

	memberOpenID := gjson.GetBytes(d, "author.member_openid").String()
	e.sender = platform.UserInfo{
		ID:          memberOpenID,
		DisplayName: gjson.GetBytes(d, "author.id").String(),
	}
	groupOpenID := gjson.GetBytes(d, "group_openid").String()
	e.chat = platform.ChatInfo{
		ID:      groupOpenID,
		IsGroup: true,
	}

	if ts := gjson.GetBytes(d, "timestamp").String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

func (e *qqEvent) populateGuildMessage() {
	if e.payload.Detail == nil {
		return
	}
	d := e.payload.Detail
	e.content = gjson.GetBytes(d, "content").String()

	e.sender = platform.UserInfo{
		ID:          gjson.GetBytes(d, "author.id").String(),
		DisplayName: gjson.GetBytes(d, "author.username").String(),
	}
	channelID := gjson.GetBytes(d, "channel_id").String()
	guildID := gjson.GetBytes(d, "guild_id").String()
	chatID := channelID
	if chatID == "" {
		chatID = guildID
	}
	e.chat = platform.ChatInfo{
		ID:      chatID,
		Name:    gjson.GetBytes(d, "channel_name").String(),
		IsGroup: true,
	}

	if ts := gjson.GetBytes(d, "timestamp").String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

// populateNoticeGroup 从群通知类事件中提取平台无关字段。
//
// 适用于：GroupAddRobot、GroupDelRobot、GroupMsgReject、GroupMsgReceive。
// 对应 dto.GroupOpRobotEvent 结构：group_openid、op_member_openid、timestamp（Unix 秒）。
func (e *qqEvent) populateNoticeGroup() {
	if e.payload.Detail == nil {
		return
	}
	d := e.payload.Detail
	groupOpenID := gjson.GetBytes(d, "group_openid").String()
	e.chat = platform.ChatInfo{
		ID:      groupOpenID,
		IsGroup: true,
	}
	opMemberOpenID := gjson.GetBytes(d, "op_member_openid").String()
	e.sender = platform.UserInfo{
		ID: opMemberOpenID,
	}
	if ts := gjson.GetBytes(d, "timestamp").Int(); ts != 0 {
		e.timestamp = time.Unix(ts, 0)
	}
}

// populateNoticeUser 从用户通知类事件中提取平台无关字段。
//
// 适用于：FriendAdd、FriendDel、C2CMsgReject、C2CMsgReceive。
// 对应 dto.UserOpRobotEvent 结构：openid、timestamp（Unix 秒）。
func (e *qqEvent) populateNoticeUser() {
	if e.payload.Detail == nil {
		return
	}
	d := e.payload.Detail
	openID := gjson.GetBytes(d, "openid").String()
	e.sender = platform.UserInfo{
		ID: openID,
	}
	e.chat = platform.ChatInfo{
		ID:      openID,
		IsGroup: false,
	}
	if ts := gjson.GetBytes(d, "timestamp").Int(); ts != 0 {
		e.timestamp = time.Unix(ts, 0)
	}
}

// --- platform.Event 接口实现 ---

func (e *qqEvent) Platform() string          { return PlatformID }
func (e *qqEvent) ID() string                { return string(e.payload.ID) }
func (e *qqEvent) Kind() platform.EventKind  { return e.kind }
func (e *qqEvent) RawType() string           { return e.payload.Type }
func (e *qqEvent) Sender() platform.UserInfo { return e.sender }
func (e *qqEvent) Chat() platform.ChatInfo   { return e.chat }
func (e *qqEvent) Content() string           { return e.content }
func (e *qqEvent) Timestamp() time.Time      { return e.timestamp }
func (e *qqEvent) RawPayload() any           { return e.payload }
