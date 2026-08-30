package messagelog

import (
	stdctx "context"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// Recorder 出站事实记录窄接口（防依赖环：调用方只需这两个方法）。
//
// 状态机：pending → sent / failed / unknown。
//   - pending：发送前记录，「bot 尝试发送了什么」这一事实的一部分；
//   - sent：平台确认发送成功（res.MessageID 可空，空不影响成功状态）；
//   - failed：平台返回错误；
//   - unknown：本地无法确定平台最终结果（如超时，平台可能已收到）——不伪造结果。
type Recorder interface {
	// RecordOutbound 在发送前记录一条 pending 出站，返回其 event_id。
	RecordOutbound(chatID string, req platform.SendRequest) string
	// RecordOutboundResult 在发送完成后更新状态（sent / failed / unknown）。
	// chatID 用于定位热缓存会话（event_id 全局唯一）。
	RecordOutboundResult(chatID, eventID string, res platform.SendResult, err error)
}

// RecordingSender 包装 platform.Sender，在发送管线层统一记录出站事实。
//
// 重要：仅用于向**注入点**装饰 sender（如 sendqueue.SetDefaultSender 传入的
// sender），不能用于包装平台适配器返回的 Sender——平台能力通过
// `a.Sender().(MessageDeleter / GroupManager / APIProvider / ...)` 类型断言暴露
// （platform/optional.go、platform/extension.go），包装会破坏这些可选接口断言。
// ctx.Reply 路径由 [Logger.OnOutbound]（OutboundObserver）覆盖，无需包装。
type RecordingSender struct {
	inner    platform.Sender
	recorder Recorder
	platform string // 平台标识（结果阶段填充 res.Platform 的兜底）
}

// NewRecordingSender 创建出站记录装饰器。
func NewRecordingSender(inner platform.Sender, recorder Recorder, platform string) *RecordingSender {
	return &RecordingSender{inner: inner, recorder: recorder, platform: platform}
}

// Send 记录 pending → 实际发送 → 记录结果。
func (s *RecordingSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	eventID := s.recorder.RecordOutbound(req.Target.ID, req)
	res, err := s.inner.Send(ctx, req)
	if res.Platform == "" {
		res.Platform = s.platform
	}
	s.recorder.RecordOutboundResult(req.Target.ID, eventID, res, err)
	return res, err
}

// outboundEntry 从 SendRequest 提取出站记录的统一字段。
// 平台/状态在结果阶段填充；content 为空时仍保留「发送过一条消息」的事实。
func outboundEntry(chatID string, req platform.SendRequest) RecordEntry {
	text := req.Message.Text
	if text == "" {
		text = req.Message.Markdown
	}
	replyTo := req.Message.ReplyToID
	return RecordEntry{
		Kind:             "OUTBOUND",
		ChatID:           chatID,
		ChatName:         req.Target.Name,
		ParentID:         req.Target.ParentID,
		IsGroup:          req.Target.IsGroup,
		Content:          text,
		ReplyToID:        replyTo,
		ReplyToMessageID: replyTo,
		Attachments:      attachmentMetas(req.Message.Attachments),
		Timestamp:        time.Now(),
		CreatedAt:        time.Now(),
		IsOutbound:       true,
		SendStatus:       SendStatusPending,
	}
}

// RecordOutbound 实现 Recorder：记录一条 pending 出站事实并返回 event_id。
func (l *Logger) RecordOutbound(chatID string, req platform.SendRequest) string {
	if chatID == "" {
		return ""
	}
	e := outboundEntry(chatID, req)
	eventID := l.Record(e)
	e.EventID = eventID // Record 在值拷贝上分配 UUID，需回填再存 pending
	l.setOutboundData(eventID, req.Message.Attachments)
	l.outboundMu.Lock()
	l.outboundPending[eventID] = e
	l.outboundMu.Unlock()
	return eventID
}

// RecordOutboundResult 实现 Recorder：将 pending 出站更新为最终状态。
//
// 幂等语义：同 event_id 的记录在 flush 时 UPSERT（见 flushLoop），
// 热缓存中旧条目由查询去重，最终状态以完成记录为准。
func (l *Logger) RecordOutboundResult(chatID, eventID string, res platform.SendResult, err error) {
	if eventID == "" {
		return
	}
	// 合并 pending 记录的全部字段（内容/会话信息），避免完成记录覆盖掉内容
	l.outboundMu.Lock()
	e, ok := l.outboundPending[eventID]
	delete(l.outboundPending, eventID)
	l.outboundMu.Unlock()
	if !ok {
		// pending 已丢失（如进程重启后重放）：退化为仅状态记录
		e = RecordEntry{EventID: eventID, ChatID: chatID, IsOutbound: true}
	}
	e.ChatID = chatID
	if res.Platform != "" {
		e.Platform = res.Platform
	}
	e.PlatformMessageID = res.MessageID
	e.CreatedAt = time.Now()
	if err != nil {
		e.SendStatus = SendStatusFailed
		e.LastError = truncateError(err)
	} else {
		e.SendStatus = SendStatusSent
	}
	l.Record(e)
}

// RecordOutboundSent 记录一条已完成（已确认发送）的出站消息。
// 供测试与已知结果的场景使用；运行时路径优先走 Recorder 或 OnOutbound。
func (l *Logger) RecordOutboundSent(chatID, platformMessageID, content string, t time.Time) {
	if chatID == "" || platformMessageID == "" || content == "" {
		return
	}
	l.Record(RecordEntry{
		Kind:              "OUTBOUND",
		PlatformMessageID: platformMessageID,
		ChatID:            chatID,
		Content:           content,
		Timestamp:         t,
		CreatedAt:         time.Now(),
		IsOutbound:        true,
		SendStatus:        SendStatusSent,
	})
}

// truncateError 截断发送错误摘要，避免大错误串膨胀数据库。
func truncateError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	const maxLen = 512
	if len(s) > maxLen {
		return strings.TrimSpace(s[:maxLen]) + "..."
	}
	return s
}
