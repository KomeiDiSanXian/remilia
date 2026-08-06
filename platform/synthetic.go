package platform

import (
	"time"

	"github.com/google/uuid"
)

// SyntheticEvent 是程序化构造的虚拟事件，实现 [Event] 接口。
//
// 主要用途：
//   - 单元/集成测试（无需真实平台连接）
//   - 插件内部向引擎注入合成事件（如定时推送、跨插件触发）
//   - 调试时模拟特定平台事件
//
// 推荐通过 [NewSyntheticEvent] + [SyntheticOption] 构造：
//
//	evt := platform.NewSyntheticEvent(
//	    platform.EventKindGroupMessage,
//	    "/ping",
//	    platform.WithSyntheticChat(platform.ChatInfo{ID: "group-1", IsGroup: true}),
//	    platform.WithSyntheticSender(platform.UserInfo{ID: "user-42"}),
//	)
type SyntheticEvent struct {
	id          string
	platformStr string
	kind        EventKind
	senderInfo  UserInfo
	chatInfo    ChatInfo
	content     string
	timestamp   time.Time
	attachments []Attachment
}

// segments 将合成事件内容包装为段列表（text → 段，附件 → 媒体段）。
func (e *SyntheticEvent) segments() []Segment {
	var segs []Segment
	if e.content != "" {
		segs = append(segs, Segment{Type: SegmentText, Text: e.content})
	}
	for _, att := range e.attachments {
		t := SegmentFile
		switch att.Kind {
		case AttachmentKindImage:
			t = SegmentImage
		case AttachmentKindAudio:
			t = SegmentAudio
		case AttachmentKindVideo:
			t = SegmentVideo
		}
		segs = append(segs, Segment{Type: t, Attachment: att})
	}
	return segs
}

// NewSyntheticEvent 创建一个合成事件。
//
// kind 指定事件类型（如 [EventKindGroupMessage]）；
// content 为消息文本；其余字段通过 opts 配置（未配置时使用合理默认值）。
func NewSyntheticEvent(kind EventKind, content string, opts ...SyntheticOption) *SyntheticEvent {
	e := &SyntheticEvent{
		id:          uuid.NewString(),
		platformStr: "synthetic",
		kind:        kind,
		content:     content,
		timestamp:   time.Now(),
	}
	for _, fn := range opts {
		fn(e)
	}
	return e
}

// SyntheticOption 用于配置 [SyntheticEvent] 的可选选项。
type SyntheticOption func(*SyntheticEvent)

// WithSyntheticPlatform 覆盖 Platform() 返回的平台名称（默认 "synthetic"）。
func WithSyntheticPlatform(p string) SyntheticOption {
	return func(e *SyntheticEvent) { e.platformStr = p }
}

// WithSyntheticID 覆盖事件 ID（默认自动生成 UUID）。
func WithSyntheticID(id string) SyntheticOption {
	return func(e *SyntheticEvent) { e.id = id }
}

// WithSyntheticSender 设置发送者信息。
func WithSyntheticSender(u UserInfo) SyntheticOption {
	return func(e *SyntheticEvent) { e.senderInfo = u }
}

// WithSyntheticChat 设置会话信息（群/私聊）。
func WithSyntheticChat(c ChatInfo) SyntheticOption {
	return func(e *SyntheticEvent) { e.chatInfo = c }
}

// WithSyntheticTimestamp 覆盖时间戳（默认 time.Now()）。
func WithSyntheticTimestamp(t time.Time) SyntheticOption {
	return func(e *SyntheticEvent) { e.timestamp = t }
}

// WithSyntheticAttachments 追加附件列表。
func WithSyntheticAttachments(a ...Attachment) SyntheticOption {
	return func(e *SyntheticEvent) { e.attachments = append(e.attachments, a...) }
}

// ── platform.Event 接口实现 ─────────────────────────────────────────────────

// Platform 返回平台标识（默认 "synthetic"，可通过 [WithSyntheticPlatform] 覆盖）。
func (e *SyntheticEvent) Platform() string { return e.platformStr }

// Kind 返回事件类别。
func (e *SyntheticEvent) Kind() EventKind { return e.kind }

// ID 返回事件唯一标识。
func (e *SyntheticEvent) ID() string { return e.id }

// Sender 返回发送者信息。
func (e *SyntheticEvent) Sender() UserInfo { return e.senderInfo }

// Chat 返回会话信息。
func (e *SyntheticEvent) Chat() ChatInfo { return e.chatInfo }

// Content 返回消息文本内容。
func (e *SyntheticEvent) Content() string { return e.content }

// Segments 返回保序统一消息段（唯一真相源，text + 媒体段）。
func (e *SyntheticEvent) Segments() []Segment { return e.segments() }

// Timestamp 返回事件时间戳。
func (e *SyntheticEvent) Timestamp() time.Time { return e.timestamp }

// Attachments 返回附件列表。
func (e *SyntheticEvent) Attachments() []Attachment { return e.attachments }
