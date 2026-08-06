package satori

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// TokenSatoriReferrer 是存储被动请求来源信息的 ChatInfo.Tokens 键。
//
// satori 适配器在解析事件时将 event.referrer 序列化后写入此键；
// satoriSender 在发送消息时读取此键并将其作为 referrer 参数传入 message.create。
//
// 参见：https://satori.chat/zh-CN/advanced/passive.html
const TokenSatoriReferrer = "satori:referrer"

// ─────────────────────────────────────────────────────────────────────────────
// Satori 事件类型字符串常量
// ─────────────────────────────────────────────────────────────────────────────

const (
	// 消息事件
	EventTypeMessageCreated = "message-created"
	EventTypeMessageUpdated = "message-updated"
	EventTypeMessageDeleted = "message-deleted"

	// 频道事件
	EventTypeChannelAdded   = "channel-added"
	EventTypeChannelUpdated = "channel-updated"
	EventTypeChannelRemoved = "channel-removed"

	// 群组事件
	EventTypeGuildAdded   = "guild-added"
	EventTypeGuildUpdated = "guild-updated"
	EventTypeGuildRemoved = "guild-removed"

	// 群组成员事件
	EventTypeGuildMemberAdded   = "guild-member-added"
	EventTypeGuildMemberUpdated = "guild-member-updated"
	EventTypeGuildMemberRemoved = "guild-member-removed"

	// 群组角色事件
	EventTypeGuildRoleCreated = "guild-role-created"
	EventTypeGuildRoleUpdated = "guild-role-updated"
	EventTypeGuildRoleDeleted = "guild-role-deleted"

	// 好友/用户事件
	EventTypeFriendRequest = "friend-request"

	// 表态事件
	EventTypeReactionAdded   = "reaction-added"
	EventTypeReactionRemoved = "reaction-removed"

	// 交互事件（实验性）
	EventTypeInteractionButton  = "interaction/button"
	EventTypeInteractionCommand = "interaction/command"

	// 登录事件
	EventTypeLoginAdded   = "login-added"
	EventTypeLoginRemoved = "login-removed"
	EventTypeLoginUpdated = "login-updated"

	// 内部/平台原生事件（实验性）
	// SDK 通过此事件类型代理未标准化的平台原生事件。
	// 参见：https://satori.chat/zh-CN/advanced/internal.html#事件扩展
	EventTypeInternal = "internal"
)

// ─────────────────────────────────────────────────────────────────────────────
// mapEventKind – Satori 事件类型字符串 → platform.EventKind
// ─────────────────────────────────────────────────────────────────────────────

// mapEventKind 将 Satori 事件类型字符串转换为框架层的 platform.EventKind 枚举，
// 使框架引擎和处理器无需感知 Satori 特定的事件字符串即可路由事件。
func mapEventKind(satoriType string, channel *Channel) platform.EventKind {
	switch satoriType {
	case EventTypeMessageCreated, EventTypeMessageUpdated:
		if satoriType == EventTypeMessageUpdated {
			return platform.EventKindMessageUpdate
		}
		// 根据频道类型区分私聊消息与群聊消息
		if channel != nil && channel.Type == ChannelTypeDirect {
			return platform.EventKindPrivateMessage
		}
		return platform.EventKindGroupMessage

	case EventTypeMessageDeleted:
		return platform.EventKindMessageDelete

	case EventTypeChannelAdded, EventTypeChannelUpdated, EventTypeChannelRemoved:
		return platform.EventKindChannelChange

	case EventTypeGuildAdded:
		return platform.EventKindBotAdded
	case EventTypeGuildRemoved:
		return platform.EventKindBotRemoved
	case EventTypeGuildUpdated:
		return platform.EventKindGuildChange

	case EventTypeGuildMemberAdded:
		return platform.EventKindMemberJoin
	case EventTypeGuildMemberRemoved:
		return platform.EventKindMemberLeave
	case EventTypeGuildMemberUpdated:
		return platform.EventKindMemberUpdate

	case EventTypeGuildRoleCreated, EventTypeGuildRoleUpdated, EventTypeGuildRoleDeleted:
		return platform.EventKindNotice

	case EventTypeFriendRequest:
		return platform.EventKindRequest

	case EventTypeReactionAdded, EventTypeReactionRemoved:
		return platform.EventKindReaction

	case EventTypeInteractionButton, EventTypeInteractionCommand:
		return platform.EventKindInteraction

	case EventTypeLoginAdded, EventTypeLoginRemoved, EventTypeLoginUpdated:
		return platform.EventKindSystem

	case EventTypeInternal:
		// 平台原生事件（实验性）：未标准化的原生事件，归类为系统事件。
		// 调用方可通过 RawPayload() 获取 *Event 并访问 NativeType/NativeData 字段。
		return platform.EventKindSystem

	default:
		return platform.EventKindUnknown
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// satoriEvent – 实现 platform.Event 接口
// ─────────────────────────────────────────────────────────────────────────────

// satoriEvent 包装 Satori *Event 并实现 platform.Event 接口，
// 使框架引擎无需感知任何 Satori 特定代码即可消费事件。
type satoriEvent struct {
	raw      *Event
	kind     platform.EventKind
	segments []platform.Segment
	platform string

	// mentions 是消息中 <at> 元素还原出的被 @ 用户，
	// replyToID 是 <quote id="..."/> 中的被回复消息 ID。
	// 两者均由 parseMessageContentFull 提供，用于实现
	// platform.MentionsEvent / platform.ReplyEvent。
	mentions  []platform.UserInfo
	replyToID string
	// botID 用于标记 mentions 中的 IsSelf，由适配器在转换时注入。
	botID string
}

// convertEvent 将原始 Satori Event 转换为 platform.Event。
func convertEvent(e *Event, platformName string) *satoriEvent {
	return convertEventWithBot(e, platformName, "")
}

// convertEventWithBot 在 convertEvent 的基础上注入机器人自身 ID，
// 使 Mentions() 能够正确标记 IsSelf —— 框架的 OnMentionedBot() 正是
// 依据该标志判定"机器人被 @"，缺少它规则将永不命中。
func convertEventWithBot(e *Event, platformName, botID string) *satoriEvent {
	kind := mapEventKind(e.Type, e.Channel)

	var parsed parsedContent
	if e.Message != nil && e.Message.Content != nil {
		parsed = parseMessageContentFull(*e.Message.Content)
	}

	mentions := parsed.Mentions
	if botID != "" {
		for i := range mentions {
			if mentions[i].ID == botID {
				mentions[i].IsSelf = true
			}
		}
	}

	// 被回复消息 ID 来自正文中的 <quote id="..."/> 元素：
	// Satori 把引用关系编码在消息内容里，Message 结构体本身没有 quote 字段。
	replyToID := parsed.QuoteID

	return &satoriEvent{
		raw:       e,
		kind:      kind,
		segments:  parsed.Segments,
		platform:  platformName,
		mentions:  mentions,
		replyToID: replyToID,
		botID:     botID,
	}
}

// Segments 实现 platform.Event，返回保序统一消息段（唯一真相源）。
func (e *satoriEvent) Segments() []platform.Segment { return e.segments }

// Content 实现 platform.Event，返回段派生文本（at/mention_all/face/quote 剥离）。
//
// 保持 satori 历史 TrimSpace 语义：段内首尾空白不进入 Content。
func (e *satoriEvent) Content() string {
	return strings.TrimSpace(platform.SegmentsContent(e.segments))
}

// Attachments 实现 platform.Event，返回段派生附件列表（image/audio/video/file）。
func (e *satoriEvent) Attachments() []platform.Attachment {
	return platform.SegmentsAttachments(e.segments)
}

// Mentions 实现 platform.MentionsEvent，返回消息中 @ 的用户。
func (e *satoriEvent) Mentions() []platform.UserInfo {
	if len(e.mentions) == 0 {
		return nil
	}
	return e.mentions
}

// ReplyToID 实现 platform.ReplyEvent，返回被回复消息的 ID。
func (e *satoriEvent) ReplyToID() string { return e.replyToID }

// Platform 返回平台标识符。
func (e *satoriEvent) Platform() string { return e.platform }

// Kind 返回平台无关的事件类别。
func (e *satoriEvent) Kind() platform.EventKind { return e.kind }

// ID 将 Satori 事件序列号以字符串形式返回作为事件唯一标识。
func (e *satoriEvent) ID() string {
	if e.raw == nil {
		return ""
	}
	return fmt.Sprintf("%d", e.raw.SN)
}

// Sender 返回事件发送者的用户信息。
func (e *satoriEvent) Sender() platform.UserInfo {
	if e.raw == nil {
		return platform.UserInfo{}
	}
	u := e.raw.User
	if u == nil && e.raw.Member != nil {
		u = e.raw.Member.User
	}
	if u == nil && e.raw.Login != nil {
		u = e.raw.Login.User
	}
	if u == nil {
		return platform.UserInfo{}
	}
	info := platform.UserInfo{ID: u.ID}
	if u.Name != nil {
		info.DisplayName = *u.Name
	}
	// User.Nick 的优先级高于 Name（见 types.go 中该字段的说明）：
	// 私聊场景没有 Member 对象，此前这一分支缺失，导致显示的是
	// "qq_user_10001" 这样的原始账号名而非好友备注。
	if u.Nick != nil && *u.Nick != "" {
		info.DisplayName = *u.Nick
	}
	// 群内昵称（Member.Nick）优先级最高。
	if nick := memberNick(e.raw.Member); nick != "" {
		info.DisplayName = nick
	}
	if u.IsBot != nil {
		info.IsBot = *u.IsBot
	}
	return info
}

// Chat 返回事件所属的会话/频道信息。
func (e *satoriEvent) Chat() platform.ChatInfo {
	if e.raw == nil {
		return platform.ChatInfo{}
	}
	info := platform.ChatInfo{}

	if ch := e.raw.Channel; ch != nil {
		info.ID = ch.ID
		if ch.Name != nil {
			info.Name = *ch.Name
		}
		info.IsGroup = ch.Type != ChannelTypeDirect
		info.IsDM = ch.Type == ChannelTypeDirect
	}
	if g := e.raw.Guild; g != nil {
		info.ParentID = g.ID
	}

	// 被动请求支持（实验性）：将 referrer 序列化存入 Tokens，
	// satoriSender 在发送回复时读取并传给 message.create API。
	if e.raw.Referrer != nil {
		if info.Tokens == nil {
			info.Tokens = make(map[string]string)
		}
		info.Tokens[TokenSatoriReferrer] = string(*e.raw.Referrer)
	}

	return info
}

// Timestamp 返回事件时间戳。
func (e *satoriEvent) Timestamp() time.Time {
	if e.raw == nil {
		return time.Time{}
	}
	return time.UnixMilli(e.raw.Timestamp)
}

// ─────────────────────────────────────────────────────────────────────────────
// 可选接口扩展
// ─────────────────────────────────────────────────────────────────────────────

// RawType 实现 platform.RawEvent 接口，返回 Satori 原始事件类型字符串。
func (e *satoriEvent) RawType() string {
	if e.raw == nil {
		return ""
	}
	return e.raw.Type
}

// RawPayload 实现 platform.RawEvent 接口，返回原始 Satori Event 对象。
func (e *satoriEvent) RawPayload() any { return e.raw }

// Referrer 返回原始 Satori 事件中的被动请求来源信息（实验性）。
//
// 调用方可将此值原样传入 Client.MessageCreateWith 以实现被动回复。
// 若事件不含 referrer 信息则返回 nil。
//
// 参见：https://satori.chat/zh-CN/advanced/passive.html
func (e *satoriEvent) Referrer() *json.RawMessage {
	if e.raw == nil {
		return nil
	}
	return e.raw.Referrer
}

// NativeType 返回平台原生事件类型（实验性）。
//
// 对于 type="internal" 的事件，此字段即原生事件类型字符串；
// 对于标准事件，此字段可能包含平台通用名称。
// 参见：https://satori.chat/zh-CN/advanced/internal.html#事件扩展
func (e *satoriEvent) NativeType() string {
	if e.raw == nil {
		return ""
	}
	return e.raw.NativeType
}

// NativeData 返回平台原生事件数据（实验性）。
//
// 参见：https://satori.chat/zh-CN/advanced/internal.html#事件扩展
func (e *satoriEvent) NativeData() *json.RawMessage {
	if e.raw == nil {
		return nil
	}
	return e.raw.NativeData
}

// ─────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// memberNick 返回群组成员的显示昵称（若存在）。
func memberNick(m *GuildMember) string {
	if m == nil {
		return ""
	}
	if m.Nick != nil && *m.Nick != "" {
		return *m.Nick
	}
	return ""
}

// 编译期接口断言，确保 satoriEvent 满足 platform 接口。
var (
	_ platform.Event         = (*satoriEvent)(nil)
	_ platform.RawEvent      = (*satoriEvent)(nil)
	_ platform.MentionsEvent = (*satoriEvent)(nil)
	_ platform.ReplyEvent    = (*satoriEvent)(nil)
)
