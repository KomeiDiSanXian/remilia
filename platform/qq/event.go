// Package qq 是 QQ 官方机器人平台的 platform.Adapter 实现。
//
// 本包将 QQ 官方数据结构（dto.Payload、dto.C2CMessageCreateEvent 等）
// 包装为 platform.Event，使框架核心不再直接依赖 QQ SDK 类型。
package qq

import (
	"encoding/json"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

const PlatformID = "qq"

// qqEvent 将 QQ 的 *dto.Payload 解析后的字段存储为平台无关类型。
//
// D5：不再持有 *dto.Payload 引用，populate 完成后立即将 payload 释放回对象池，
// 避免事件对象长期持有池资源，降低 GC 压力。
type qqEvent struct {
	kind        platform.EventKind
	sender      platform.UserInfo
	chat        platform.ChatInfo
	content     string
	timestamp   time.Time
	attachments []platform.InboundAttachment
	id          string // 对应 payload.ID，populate 后独立持有
	rawType     string // 对应 payload.Type，populate 后独立持有
	replyToID   string // 被回复消息 ID（仅频道消息有效）
}

// NewEvent 从 QQ payload 创建 platform.Event。
//
// D5：提取所有字段后立即调用 dto.ReleasePayload，将 payload 还给对象池。
// 调用方在 NewEvent 返回后不得再访问 payload。
func NewEvent(payload *dto.Payload) platform.Event {
	e := &qqEvent{}
	if payload == nil {
		e.kind = platform.EventKindUnknown
		return e
	}
	// 在 populate 前提取不依赖 Detail 的字段
	e.id = string(payload.ID)
	e.rawType = payload.Type
	e.populateFrom(payload.Type, payload.Detail)
	// 所有字段已拷贝到 e，立即释放 payload 回对象池
	dto.ReleasePayload(payload)
	return e
}

// populateFrom 根据事件类型解析 detail 字节并填充 e 的字段。
func (e *qqEvent) populateFrom(evType string, detail json.RawMessage) {
	switch evType {
	case dto.C2CMessageCreate:
		e.kind = platform.EventKindPrivateMessage
		e.populateC2C(detail)
	case dto.GroupAtMessageCreate:
		e.kind = platform.EventKindGroupMessage
		e.populateGroupAt(detail)
	case dto.Ready, dto.Resumed:
		e.kind = platform.EventKindSystem
	case dto.GroupAddRobot:
		e.kind = platform.EventKindMemberJoin
		e.populateNoticeGroup(detail)
	case dto.GroupDelRobot:
		e.kind = platform.EventKindMemberLeave
		e.populateNoticeGroup(detail)
	case dto.GroupMsgReject, dto.GroupMsgReceive:
		e.kind = platform.EventKindNotice
		e.populateNoticeGroup(detail)
	case dto.FriendAdd:
		e.kind = platform.EventKindMemberJoin
		e.populateNoticeUser(detail)
	case dto.FriendDel:
		e.kind = platform.EventKindMemberLeave
		e.populateNoticeUser(detail)
	case dto.C2CMsgReject, dto.C2CMsgReceive:
		e.kind = platform.EventKindNotice
		e.populateNoticeUser(detail)
	case dto.AtMessageCreate, dto.MessageCreate, dto.DirectMessageCreate:
		e.kind = platform.EventKindGuildMessage
		e.populateGuildMessage(evType, detail)
	case dto.InteractionCreate:
		e.kind = platform.EventKindInteraction
	// ── 频道（Guild）事件 ──────────────────────────────────────────────────
	case dto.GuildCreate:
		e.kind = platform.EventKindMemberJoin // 机器人加入频道
		e.populateGuildEvent(detail)
	case dto.GuildUpdate:
		e.kind = platform.EventKindNotice
		e.populateGuildEvent(detail)
	case dto.GuildDelete:
		e.kind = platform.EventKindMemberLeave // 机器人退出频道
		e.populateGuildEvent(detail)
	// ── 频道成员事件 ────────────────────────────────────────────────────────
	case dto.GuildMemberAdd:
		e.kind = platform.EventKindMemberJoin
		e.populateGuildMemberEvent(detail)
	case dto.GuildMemberUpdate:
		e.kind = platform.EventKindNotice
		e.populateGuildMemberEvent(detail)
	case dto.GuildMemberRemove:
		e.kind = platform.EventKindMemberLeave
		e.populateGuildMemberEvent(detail)
	// ── 子频道事件 ──────────────────────────────────────────────────────────
	case dto.ChannelCreate, dto.ChannelUpdate, dto.ChannelDelete:
		e.kind = platform.EventKindNotice
		e.populateChannelEvent(detail)
	// ── 消息撤回事件 ────────────────────────────────────────────────────────
	case dto.MessageDeleteEvent:
		e.kind = platform.EventKindMessageDelete
		e.populateMessageDelete(detail)
	default:
		e.kind = platform.EventKindUnknown
	}
}

func (e *qqEvent) populateC2C(detail json.RawMessage) {
	if detail == nil {
		return
	}
	// 一次线性扫描提取所有字段（O(n) 而非 O(k×n)）
	results := gjson.GetManyBytes(detail,
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

func (e *qqEvent) populateGroupAt(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"content",
		"author.member_openid",
		"author.id",
		"group_openid",
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
		IsGroup: true,
	}
	if ts := results[4].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
	e.attachments = parseAttachments(results[5])
}

func (e *qqEvent) populateGuildMessage(evType string, detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"content",
		"author.id",
		"author.username",
		"channel_id",
		"guild_id",
		"channel_name",
		"timestamp",
		"attachments",
		"message_reference.message_id",
	)
	e.content = results[0].String()
	e.sender = platform.UserInfo{
		ID:          results[1].String(),
		DisplayName: results[2].String(),
	}
	channelID := results[3].String()
	guildID := results[4].String()

	isDM := evType == dto.DirectMessageCreate
	if isDM {
		// 频道私信：以 guild_id 作为发送目标（POST /dms/{guild_id}/messages），
		// channel_id 仅作辅助信息，不用于发送。
		e.chat = platform.ChatInfo{
			ID:       guildID, // 私信会话 ID（用于 DMChat）
			ParentID: guildID,
			Name:     results[5].String(),
			IsGroup:  false,
			IsDM:     true,
		}
	} else {
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
	}

	if ts := results[6].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
	e.attachments = parseAttachments(results[7])
	e.replyToID = results[8].String()
}

// populateGuildEvent 解析频道事件（机器人加入/退出/更新频道）。
func (e *qqEvent) populateGuildEvent(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"id",
		"name",
		"owner_id",
		"joined_at",
	)
	guildID := results[0].String()
	e.chat = platform.ChatInfo{
		ID:      guildID,
		Name:    results[1].String(),
		IsGroup: true,
	}
	e.sender = platform.UserInfo{
		ID: results[2].String(),
	}
	if ts := results[3].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

// populateGuildMemberEvent 解析频道成员事件（加入/更新/移除成员）。
func (e *qqEvent) populateGuildMemberEvent(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"guild_id",
		"user.id",
		"user.username",
		"nick",
		"joined_at",
		"op_user_id",
	)
	e.chat = platform.ChatInfo{
		ID:      results[0].String(),
		IsGroup: true,
	}
	e.sender = platform.UserInfo{
		ID:          results[1].String(),
		DisplayName: results[2].String(),
	}
	if ts := results[4].String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.timestamp = t
		}
	}
}

// populateChannelEvent 解析子频道事件（创建/更新/删除子频道）。
func (e *qqEvent) populateChannelEvent(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"id",
		"guild_id",
		"name",
		"op_user_id",
	)
	e.chat = platform.ChatInfo{
		ID:       results[0].String(),
		ParentID: results[1].String(),
		Name:     results[2].String(),
		IsGroup:  true,
	}
	e.sender = platform.UserInfo{
		ID: results[3].String(),
	}
}

// populateMessageDelete 解析频道消息撤回事件。
func (e *qqEvent) populateMessageDelete(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
		"message.id",
		"message.channel_id",
		"message.guild_id",
		"op_user.id",
	)
	e.id = results[0].String()
	e.chat = platform.ChatInfo{
		ID:       results[1].String(),
		ParentID: results[2].String(),
		IsGroup:  true,
	}
	e.sender = platform.UserInfo{
		ID: results[3].String(),
	}
}

// parseAttachments 将 gjson 数组结果转换为平台无关的 InboundAttachment 切片。
//
// 使用 r.Array() 预获取元素数量，一次性分配输出切片，避免 append 扩容。
func parseAttachments(r gjson.Result) []platform.InboundAttachment {
	if !r.IsArray() || len(r.Raw) == 0 {
		return nil
	}
	arr := r.Array()
	if len(arr) == 0 {
		return nil
	}
	out := make([]platform.InboundAttachment, 0, len(arr))
	for _, v := range arr {
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
	}
	return out
}

func (e *qqEvent) populateNoticeGroup(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
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

func (e *qqEvent) populateNoticeUser(detail json.RawMessage) {
	if detail == nil {
		return
	}
	results := gjson.GetManyBytes(detail,
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

func (e *qqEvent) Platform() string          { return PlatformID }
func (e *qqEvent) ID() string                { return e.id }
func (e *qqEvent) Kind() platform.EventKind  { return e.kind }
func (e *qqEvent) RawType() string           { return e.rawType }
func (e *qqEvent) Sender() platform.UserInfo { return e.sender }
func (e *qqEvent) Chat() platform.ChatInfo   { return e.chat }
func (e *qqEvent) Content() string           { return e.content }

// Attachments 返回消息中携带的附件列表。
func (e *qqEvent) Attachments() []platform.InboundAttachment { return e.attachments }
func (e *qqEvent) Timestamp() time.Time                      { return e.timestamp }

// ReplyToID 实现 platform.ReplyEvent，返回被回复消息的平台原生 ID。
//
// 仅频道消息（AT_MESSAGE_CREATE 等）填充此字段；
// 私聊和群消息不携带 message_reference，返回空字符串。
func (e *qqEvent) ReplyToID() string { return e.replyToID }

// RawPayload 返回 nil。
//
// D5：payload 在 populate 完成后已立即释放回对象池，不再长期持有引用。
// 需要访问 QQ 平台特定字段的代码应在 handler 内通过其他上下文获取。
func (e *qqEvent) RawPayload() any { return nil }
