package onebot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// PlatformID 是 OneBot V11 的平台标识符。
const PlatformID = "onebot"

// ────────────────────────────────────────────────────────────────────────────
// onebotEvent — platform.Event 实现
// ────────────────────────────────────────────────────────────────────────────

// onebotEvent 将解析后的 OneBot V11 事件数据存储为 platform.Event。
//
// 实现了核心 platform.Event 接口以及可选扩展接口：
// RawEvent、ReplyEvent、MentionsEvent。
type onebotEvent struct {
	kind       platform.EventKind
	senderInfo platform.UserInfo
	chat       platform.ChatInfo
	segments   []platform.Segment
	timestamp  time.Time
	id         string
	rawType    string
	rawPayload any
	// botID 机器人自身 ID（来自事件 self_id），用于 Mentions() 的 IsSelf 判定。
	botID string
}

// ── platform.Event ──────────────────────────────────────────────────────────

func (e *onebotEvent) Platform() string             { return PlatformID }
func (e *onebotEvent) Kind() platform.EventKind     { return e.kind }
func (e *onebotEvent) ID() string                   { return e.id }
func (e *onebotEvent) Sender() platform.UserInfo    { return e.senderInfo }
func (e *onebotEvent) Chat() platform.ChatInfo      { return e.chat }
func (e *onebotEvent) Timestamp() time.Time         { return e.timestamp }
func (e *onebotEvent) Segments() []platform.Segment { return e.segments }

// ── platform.RawEvent ───────────────────────────────────────────────────────

func (e *onebotEvent) RawType() string { return e.rawType }
func (e *onebotEvent) RawPayload() any { return e.rawPayload }

// ── platform.ReplyEvent ─────────────────────────────────────────────────────
//
// Reply 单一真相源：委托段查找，杜绝与段双写。

func (e *onebotEvent) ReplyToID() string { return segmentsReplyToID(e.segments) }

// ── platform.MentionsEvent ──────────────────────────────────────────────────
//
// 聚合视图：由段派生（保序去重）；IsSelf 以事件 self_id 判定（@ 机器人自身命中）。

func (e *onebotEvent) Mentions() []platform.UserInfo { return segmentsToMentions(e.segments, e.botID) }

// ────────────────────────────────────────────────────────────────────────────
// 顶层事件解析
// ────────────────────────────────────────────────────────────────────────────

// parseEvent 将原始 JSON 字节反序列化为 platform.Event。
//
// 这是所有适配器（WS 正向、WS 反向、HTTP POST）在收到 OneBot 实现的新事件时
// 调用的统一入口。
func parseEvent(raw []byte) (platform.Event, error) {
	// 快速路径：不做完整解析，仅提取 post_type
	var base struct {
		PostType string `json:"post_type"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, fmt.Errorf("onebot: failed to parse event base: %w", err)
	}

	switch base.PostType {
	case PostTypeMessage, PostTypeMessageSent:
		return parseMessageEvent(raw)
	case PostTypeNotice:
		return parseNoticeEvent(raw)
	case PostTypeRequest:
		return parseRequestEvent(raw)
	case PostTypeMetaEvent:
		return parseMetaEvent(raw)
	default:
		return parseUnknownEvent(raw, base.PostType), nil
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 消息事件解析
// ────────────────────────────────────────────────────────────────────────────

func parseMessageEvent(raw []byte) (platform.Event, error) {
	var msgType struct {
		MessageType string `json:"message_type"`
	}
	if err := json.Unmarshal(raw, &msgType); err != nil {
		return nil, fmt.Errorf("onebot: failed to parse message_type: %w", err)
	}

	switch msgType.MessageType {
	case MessageTypePrivate:
		return parsePrivateMessageEvent(raw)
	case MessageTypeGroup:
		return parseGroupMessageEvent(raw)
	default:
		return parseUnknownEvent(raw, "message/"+msgType.MessageType), nil
	}
}

func parsePrivateMessageEvent(raw []byte) (platform.Event, error) {
	var ev PrivateMessageEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("onebot: failed to parse private message event: %w", err)
	}

	e := &onebotEvent{
		kind:       platform.EventKindPrivateMessage,
		rawType:    "message/private/" + ev.SubType,
		rawPayload: &ev,
		id:         strconv.FormatInt(int64(ev.MessageID), 10),
		timestamp:  time.Unix(ev.Time, 0),
		segments:   ev.Message.Segments(),
		botID:      strconv.FormatInt(ev.SelfID, 10), // self_id 用于 @ 机器人判定
		senderInfo: platform.UserInfo{
			ID:          strconv.FormatInt(ev.UserID, 10),
			DisplayName: ev.Sender.Nickname,
		},
		chat: platform.ChatInfo{
			ID:      strconv.FormatInt(ev.UserID, 10),
			IsGroup: false,
			IsDM:    true,
		},
	}

	return e, nil
}

func parseGroupMessageEvent(raw []byte) (platform.Event, error) {
	var ev GroupMessageEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("onebot: failed to parse group message event: %w", err)
	}

	displayName := ev.Sender.Card
	if displayName == "" {
		displayName = ev.Sender.Nickname
	}

	role := platform.GroupRoleMember
	switch ev.Sender.Role {
	case "owner":
		role = platform.GroupRoleOwner
	case "admin":
		role = platform.GroupRoleAdmin
	}

	e := &onebotEvent{
		kind:       platform.EventKindGroupMessage,
		rawType:    "message/group/" + ev.SubType,
		rawPayload: &ev,
		id:         strconv.FormatInt(int64(ev.MessageID), 10),
		timestamp:  time.Unix(ev.Time, 0),
		segments:   ev.Message.Segments(),
		botID:      strconv.FormatInt(ev.SelfID, 10), // self_id 用于 @ 机器人判定
		senderInfo: platform.UserInfo{
			ID:          strconv.FormatInt(ev.UserID, 10),
			DisplayName: displayName,
			GroupRole:   role,
		},
		chat: platform.ChatInfo{
			ID:      strconv.FormatInt(ev.GroupID, 10),
			IsGroup: true,
		},
	}

	// Token 用于群消息回复路由：存储 message_id 以便发送端使用 group_id 和 message_id
	e.chat.Tokens = map[string]string{
		TokenGroupID:   strconv.FormatInt(ev.GroupID, 10),
		TokenMessageID: strconv.FormatInt(int64(ev.MessageID), 10),
	}

	return e, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 通知事件解析
// ────────────────────────────────────────────────────────────────────────────

func parseNoticeEvent(raw []byte) (platform.Event, error) {
	var ev NoticeEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("onebot: failed to parse notice event: %w", err)
	}

	e := &onebotEvent{
		rawType:    "notice/" + ev.NoticeType,
		rawPayload: &ev,
		timestamp:  time.Unix(ev.Time, 0),
		id:         fmt.Sprintf("notice/%s/%d", ev.NoticeType, ev.Time),
		senderInfo: platform.UserInfo{
			ID: strconv.FormatInt(ev.UserID, 10),
		},
	}

	// Map notice_type (and sub_type) to platform.EventKind
	switch ev.NoticeType {
	case NoticeTypeGroupUpload:
		e.kind = platform.EventKindNotice
		e.rawType = "notice/group_upload"
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeGroupAdmin:
		e.kind = platform.EventKindNotice
		e.rawType = "notice/group_admin/" + ev.SubType
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeGroupDecrease:
		e.rawType = "notice/group_decrease/" + ev.SubType
		e.chat = groupChat(ev.GroupID)
		if ev.SubType == GroupDecreaseKickMe {
			e.kind = platform.EventKindBotRemoved
		} else {
			e.kind = platform.EventKindMemberLeave
		}

	case NoticeTypeGroupIncrease:
		e.kind = platform.EventKindMemberJoin
		e.rawType = "notice/group_increase/" + ev.SubType
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeGroupBan:
		e.kind = platform.EventKindNotice
		e.rawType = "notice/group_ban/" + ev.SubType
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeFriendAdd:
		e.kind = platform.EventKindFriendAdded
		e.rawType = "notice/friend_add"
		e.chat = platform.ChatInfo{
			ID:      strconv.FormatInt(ev.UserID, 10),
			IsGroup: false,
			IsDM:    true,
		}

	case NoticeTypeFriendRemove:
		e.kind = platform.EventKindFriendRemoved
		e.rawType = "notice/friend_remove"
		e.chat = platform.ChatInfo{
			ID:      strconv.FormatInt(ev.UserID, 10),
			IsGroup: false,
			IsDM:    true,
		}

	case NoticeTypeGroupRecall:
		e.kind = platform.EventKindMessageDelete
		e.rawType = "notice/group_recall"
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeFriendRecall:
		e.kind = platform.EventKindMessageDelete
		e.rawType = "notice/friend_recall"
		e.chat = platform.ChatInfo{
			ID:      strconv.FormatInt(ev.UserID, 10),
			IsGroup: false,
			IsDM:    true,
		}

	case NoticeTypeNotify:
		e.kind = platform.EventKindNotice
		e.rawType = "notice/notify/" + ev.SubType
		e.chat = groupChat(ev.GroupID)
		switch ev.SubType {
		case NotifySubTypePoke:
			e.setNoticeContent(fmt.Sprintf("poke:%s→%s",
				strconv.FormatInt(ev.UserID, 10),
				strconv.FormatInt(ev.TargetID, 10)))
		case NotifySubTypeLuckyKing:
			e.setNoticeContent(fmt.Sprintf("lucky_king:%s",
				strconv.FormatInt(ev.TargetID, 10)))
		case NotifySubTypeHonor:
			e.setNoticeContent(fmt.Sprintf("honor:%s:%s",
				strconv.FormatInt(ev.UserID, 10), ev.HonorType))
		case NotifySubTypePokeRecall: // LLOneBot/LuckyLilliaBot 扩展
			e.setNoticeContent(fmt.Sprintf("poke_recall:%s→%s",
				strconv.FormatInt(ev.UserID, 10),
				strconv.FormatInt(ev.TargetID, 10)))
		case NotifySubTypeTitle: // LLOneBot/LuckyLilliaBot 扩展
			e.setNoticeContent(ev.Title)
		case NotifySubTypeProfileLike: // LLOneBot/LuckyLilliaBot 扩展
			e.setNoticeContent(fmt.Sprintf("profile_like:%s×%d",
				strconv.FormatInt(ev.OperatorID, 10), ev.Times))
		}

	// ── LLOneBot / LuckyLilliaBot 扩展通知 ─────────────────────────────────

	case NoticeTypeGroupCard:
		e.kind = platform.EventKindMemberUpdate
		e.rawType = "notice/group_card"
		e.chat = groupChat(ev.GroupID)
		e.setNoticeContent(fmt.Sprintf("card:%s→%s", ev.CardOld, ev.CardNew))

	case NoticeTypeGroupDismiss:
		e.kind = platform.EventKindNotice
		e.rawType = "notice/group_dismiss"
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeEssence:
		e.kind = platform.EventKindNotice
		e.rawType = "notice/essence/" + ev.SubType
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeGroupMsgEmojiLike:
		e.kind = platform.EventKindReaction
		e.rawType = "notice/group_msg_emoji_like"
		e.chat = groupChat(ev.GroupID)

	case NoticeTypeFlashFile:
		e.kind = platform.EventKindNotice
		e.rawType = "notice/flash_file/" + ev.SubType
		e.chat = groupChat(ev.GroupID)

	default:
		e.kind = platform.EventKindNotice
	}

	return e, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 请求事件解析
// ────────────────────────────────────────────────────────────────────────────

func parseRequestEvent(raw []byte) (platform.Event, error) {
	var ev RequestEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("onebot: failed to parse request event: %w", err)
	}

	e := &onebotEvent{
		kind:       platform.EventKindRequest,
		rawType:    "request/" + ev.RequestType,
		rawPayload: &ev,
		timestamp:  time.Unix(ev.Time, 0),
		id:         fmt.Sprintf("request/%s/%d", ev.RequestType, ev.Time),
		segments:   textSegments(ev.Comment),
		senderInfo: platform.UserInfo{
			ID: strconv.FormatInt(ev.UserID, 10),
		},
	}

	switch ev.RequestType {
	case RequestTypeFriend:
		e.chat = platform.ChatInfo{
			ID:      strconv.FormatInt(ev.UserID, 10),
			IsGroup: false,
			IsDM:    true,
		}
	case RequestTypeGroup:
		e.rawType = "request/group/" + ev.SubType
		e.chat = groupChat(ev.GroupID)
	}

	// 存储 flag token 以便 InvitationHandler 使用
	e.chat.Tokens = map[string]string{
		TokenRequestFlag: ev.Flag,
		TokenRequestType: ev.RequestType,
		TokenRequestSub:  ev.SubType,
	}

	return e, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 元事件解析
// ────────────────────────────────────────────────────────────────────────────

func parseMetaEvent(raw []byte) (platform.Event, error) {
	var ev MetaEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("onebot: failed to parse meta event: %w", err)
	}

	e := &onebotEvent{
		kind:       platform.EventKindSystem,
		rawType:    "meta_event/" + ev.MetaEventType,
		rawPayload: &ev,
		timestamp:  time.Unix(ev.Time, 0),
		id:         fmt.Sprintf("meta/%s/%d", ev.MetaEventType, ev.Time),
		senderInfo: platform.UserInfo{
			ID: strconv.FormatInt(ev.SelfID, 10),
		},
	}

	switch ev.MetaEventType {
	case MetaEventTypeLifecycle:
		e.rawType = "meta_event/lifecycle/" + ev.SubType
		e.setNoticeContent(ev.SubType)
	case MetaEventTypeHeartbeat:
		e.setNoticeContent("heartbeat")
	}

	return e, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 未知事件兜底
// ────────────────────────────────────────────────────────────────────────────

func parseUnknownEvent(raw []byte, rawType string) platform.Event {
	return &onebotEvent{
		kind:       platform.EventKindUnknown,
		rawType:    rawType,
		rawPayload: json.RawMessage(raw),
		timestamp:  time.Now(),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ────────────────────────────────────────────────────────────────────────────

// groupChat 根据群 ID 构造 ChatInfo。
func groupChat(groupID int64) platform.ChatInfo {
	return platform.ChatInfo{
		ID:      strconv.FormatInt(groupID, 10),
		IsGroup: true,
	}
}

// textSegments 将纯文本包装为单条 text 段。
func textSegments(text string) []platform.Segment {
	if text == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: text}}
}

// setNoticeContent 为通知/元事件设置文本内容（以 text 段存储，Content() 派生一致）。
func (e *onebotEvent) setNoticeContent(text string) {
	e.segments = textSegments(text)
}

// ────────────────────────────────────────────────────────────────────────────
// Token 键常量
// ────────────────────────────────────────────────────────────────────────────

// ChatInfo.Tokens 中存储的平台特定操作 token 键。
const (
	// TokenGroupID 是群消息回复路由中存储的群 ID 字符串。
	TokenGroupID = "group_id"
	// TokenMessageID 是用于关联 API 调用的 message_id。
	TokenMessageID = "message_id"
	// TokenRequestFlag 是请求事件中的 flag，处理请求时需要使用。
	TokenRequestFlag = "request_flag"
	// TokenRequestType 是请求类型（"friend" 或 "group"）。
	TokenRequestType = "request_type"
	// TokenRequestSub 是群请求的子类型（"add" 或 "invite"）。
	TokenRequestSub = "request_sub"
)
