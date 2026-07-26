package terminal

import (
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// Event 表示一条终端输入事件。
type Event struct {
	id         string
	kind       platform.EventKind
	content    string
	senderID   string
	senderName string
	timestamp  time.Time
	chatID     string
	isGroup    bool
	rawType    string
	replyToID  string
	mentions   []platform.UserInfo
}

// NewEvent 创建一条私聊终端事件。
func NewEvent(content string) *Event {
	return &Event{
		id:         generateEventID(),
		kind:       platform.EventKindPrivateMessage,
		content:    content,
		senderID:   DefaultUserID,
		senderName: "Terminal User",
		timestamp:  time.Now(),
		chatID:     "terminal-chat",
		isGroup:    false,
	}
}

// NewGroupEvent 创建一条群组终端事件。
func NewGroupEvent(content string, groupID string) *Event {
	return &Event{
		id:         generateEventID(),
		kind:       platform.EventKindGroupMessage,
		content:    content,
		senderID:   DefaultUserID,
		senderName: "Terminal User",
		timestamp:  time.Now(),
		chatID:     groupID,
		isGroup:    true,
	}
}

// SetKind 设置事件类型（用于测试不同类型的事件）。
func (e *Event) SetKind(kind platform.EventKind) *Event {
	e.kind = kind
	return e
}

// SetSender 设置发送者信息。
func (e *Event) SetSender(id, name string) *Event {
	e.senderID = id
	e.senderName = name
	return e
}

// SetRawType 设置原始事件类型字符串（实现 platform.RawEvent）。
func (e *Event) SetRawType(rt string) *Event {
	e.rawType = rt
	return e
}

// SetReplyToID 设置被回复消息的 ID（实现 platform.ReplyEvent）。
func (e *Event) SetReplyToID(id string) *Event {
	e.replyToID = id
	return e
}

// SetMentions 设置 @ 用户列表（实现 platform.MentionsEvent）。
func (e *Event) SetMentions(m []platform.UserInfo) *Event {
	e.mentions = m
	return e
}

// Platform 返回平台标识符。
func (e *Event) Platform() string {
	return PlatformID
}

// Kind 返回事件类型。
func (e *Event) Kind() platform.EventKind {
	return e.kind
}

// ID 返回事件唯一标识符。
func (e *Event) ID() string {
	return e.id
}

// Content 返回消息文本内容。
func (e *Event) Content() string {
	return e.content
}

// Attachments 返回附件列表（终端始终为 nil）。
func (e *Event) Attachments() []platform.InboundAttachment {
	return nil
}

// Sender 返回消息发送者信息。
func (e *Event) Sender() platform.UserInfo {
	return platform.UserInfo{
		ID:          e.senderID,
		DisplayName: e.senderName,
		IsBot:       false,
		GroupRole:   platform.GroupRoleMember,
	}
}

// Chat 返回消息所在会话信息。
func (e *Event) Chat() platform.ChatInfo {
	return platform.ChatInfo{
		ID:      e.chatID,
		IsGroup: e.isGroup,
		Name:    "Terminal",
	}
}

// Timestamp 返回事件时间戳。
func (e *Event) Timestamp() time.Time {
	return e.timestamp
}

// ── platform.RawEvent 实现 ──────────────────────────────────────────────────

// RawType 返回平台原始事件类型字符串。
func (e *Event) RawType() string {
	return e.rawType
}

// RawPayload 返回原始 payload（终端无原始 payload，返回 nil）。
func (e *Event) RawPayload() any {
	return nil
}

// ── platform.EditableEvent 实现 ─────────────────────────────────────────────

// IsEdited 终端事件始终未被编辑。
func (e *Event) IsEdited() bool {
	return false
}

// OriginalTimestamp 返回原始时间戳（终端与 Timestamp 相同）。
func (e *Event) OriginalTimestamp() time.Time {
	return e.timestamp
}

// ── platform.ReplyEvent 实现 ────────────────────────────────────────────────

// ReplyToID 返回被回复消息的 ID。
func (e *Event) ReplyToID() string {
	return e.replyToID
}

// ── platform.MentionsEvent 实现 ─────────────────────────────────────────────

// Mentions 返回消息中 @ 的用户列表。
func (e *Event) Mentions() []platform.UserInfo {
	return e.mentions
}

// eventCounter 用于生成唯一事件 ID 的计数器。
//
// 必须使用原子类型：NewEvent / NewGroupEvent 以及导出的测试辅助方法
// SimulateMessage / SimulateGroupMessage 都可能被多个 goroutine 并发调用。
// 普通的 ++ 是非同步的读-改-写（-race 直接报警），丢失更新还会让两个不同
// 事件拿到同一个 ID，破坏一切以事件 ID 为键的去重与关联逻辑。
// 同结构体内的 Adapter.msgCount 已经是 atomic.Uint64。
var eventCounter atomic.Uint64

// generateEventID 生成唯一的事件 ID。
func generateEventID() string {
	return "terminal-" + formatUint64(eventCounter.Add(1))
}

// formatUint64 将 uint64 格式化为字符串（避免依赖 strconv）。
func formatUint64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
