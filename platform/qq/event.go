// Package qq 是 QQ 官方机器人平台的 platform.Adapter 实现。
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
	payload     *dto.Payload
	kind        platform.EventKind
	sender      platform.UserInfo
	chat        platform.ChatInfo
	content     string
	timestamp   time.Time
	attachments []platform.InboundAttachment
}

// NewEvent 从 QQ payload 创建 platform.Event
func NewEvent(payload *dto.Payload) platform.Event {
	e := &qqEvent{payload: payload}
	e.populate()
	return e
}

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
	case dto.GroupAddRobot:
		e.kind = platform.EventKindMemberJoin
		e.populateNoticeGroup()
	case dto.GroupDelRobot:
		e.kind = platform.EventKindMemberLeave
		e.populateNoticeGroup()
	case dto.GroupMsgReject, dto.GroupMsgReceive:
		e.kind = platform.EventKindNotice
		e.populateNoticeGroup()
	case dto.FriendAdd:
		e.kind = platform.EventKindMemberJoin
		e.populateNoticeUser()
	case dto.FriendDel:
		e.kind = platform.EventKindMemberLeave
		e.populateNoticeUser()
	case dto.C2CMsgReject, dto.C2CMsgReceive:
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
	// 一次线性扫描提取所有字段（O(n) 而非 O(k×n)）
	results := gjson.GetManyBytes(e.payload.Detail,
		"content",
		"author.user_openid",
		"author.id",
		"timestamp",
		"attachments",
	)
	e.content = results[0].String()
	userOpenID := results[1].String()
	e.sender = platform.UserInfo{
		ID:          userOpenID,
		DisplayName: results[2].String(),
	}
	e.chat = platform.ChatInfo{
		ID:      userOpenID,
		IsGroup: false,
	}
	if ts := results[3].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
	e.attachments = parseAttachments(results[4])
}

func (e *qqEvent) populateGroupAt() {
	if e.payload.Detail == nil {
		return
	}
	results := gjson.GetManyBytes(e.payload.Detail,
		"content",
		"author.member_openid",
		"author.id",
		"group_openid",
		"group_name",
		"timestamp",
		"attachments",
	)
	e.content = results[0].String()
	memberOpenID := results[1].String()
	e.sender = platform.UserInfo{
		ID:          memberOpenID,
		DisplayName: results[2].String(),
	}
	e.chat = platform.ChatInfo{
		ID:      results[3].String(),
		Name:    results[4].String(),
		IsGroup: true,
	}
	if ts := results[5].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
	e.attachments = parseAttachments(results[6])
}

func (e *qqEvent) populateGuildMessage() {
	if e.payload.Detail == nil {
		return
	}
	results := gjson.GetManyBytes(e.payload.Detail,
		"content",
		"author.id",
		"author.username",
		"channel_id",
		"guild_id",
		"channel_name",
		"timestamp",
		"attachments",
	)
	e.content = results[0].String()
	e.sender = platform.UserInfo{
		ID:          results[1].String(),
		DisplayName: results[2].String(),
	}
	channelID := results[3].String()
	guildID := results[4].String()
	chatID := channelID
	if chatID == "" {
		chatID = guildID
	}
	e.chat = platform.ChatInfo{
		ID:       chatID,
		ParentID: guildID,
		Name:     results[5].String(),
		IsGroup:  true,
	}
	if ts := results[6].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
	e.attachments = parseAttachments(results[7])
}

// parseAttachments 将 gjson 数组结果转换为平台无关的 InboundAttachment 切片。
func parseAttachments(r gjson.Result) []platform.InboundAttachment {
	if !r.IsArray() || len(r.Raw) == 0 {
		return nil
	}
	var out []platform.InboundAttachment
	r.ForEach(func(_, v gjson.Result) bool {
		att := platform.InboundAttachment{
			URL:      v.Get("url").String(),
			MimeType: v.Get("content_type").String(),
			Name:     v.Get("filename").String(),
			Size:     int(v.Get("size").Int()),
			Width:    int(v.Get("width").Int()),
			Height:   int(v.Get("height").Int()),
		}
		if att.URL != "" || att.Name != "" {
			out = append(out, att)
		}
		return true
	})
	return out
}

func (e *qqEvent) populateNoticeGroup() {
	if e.payload.Detail == nil {
		return
	}
	results := gjson.GetManyBytes(e.payload.Detail,
		"group_openid",
		"op_member_openid",
		"timestamp",
	)
	e.chat = platform.ChatInfo{
		ID:      results[0].String(),
		IsGroup: true,
	}
	e.sender = platform.UserInfo{
		ID: results[1].String(),
	}
	if ts := results[2].Int(); ts != 0 {
		e.timestamp = time.Unix(ts, 0)
	}
}

func (e *qqEvent) populateNoticeUser() {
	if e.payload.Detail == nil {
		return
	}
	results := gjson.GetManyBytes(e.payload.Detail,
		"openid",
		"timestamp",
	)
	openID := results[0].String()
	e.sender = platform.UserInfo{ID: openID}
	e.chat = platform.ChatInfo{ID: openID, IsGroup: false}
	if ts := results[1].Int(); ts != 0 {
		e.timestamp = time.Unix(ts, 0)
	}
}

func (e *qqEvent) Platform() string { return PlatformID }
func (e *qqEvent) ID() string {
	if e.payload == nil {
		return ""
	}
	return string(e.payload.ID)
}
func (e *qqEvent) Kind() platform.EventKind { return e.kind }
func (e *qqEvent) RawType() string {
	if e.payload == nil {
		return ""
	}
	return e.payload.Type
}
func (e *qqEvent) Sender() platform.UserInfo                 { return e.sender }
func (e *qqEvent) Chat() platform.ChatInfo                   { return e.chat }
func (e *qqEvent) Content() string                           { return e.content }
func (e *qqEvent) Attachments() []platform.InboundAttachment { return e.attachments }
func (e *qqEvent) Timestamp() time.Time                      { return e.timestamp }
func (e *qqEvent) RawPayload() any                           { return e.payload }
