package qq

import (
	stdctx "context"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// qqSender 将 platform.Sender 接口桥接到 openapi.OpenAPI
type qqSender struct {
	api openapi.OpenAPI
}

// NewSender 创建 QQ 平台的消息发送器
func NewSender(api openapi.OpenAPI) platform.Sender {
	return &qqSender{api: api}
}

// Send 将 OutboundMessage 转换并发送到 QQ 平台。
//
// 目标会话信息从 ctx 的 ChatInfo 读取（由 Reply 或 platform.WithChatInfo 注入）。
// 若 ctx 未携带 ChatInfo，返回 errutil.ErrNoChatInfo。
//
// 路由规则：
//   - ChatInfo.ParentID 非空 → 频道消息（guild/channel），当前暂不支持，返回错误
//   - ChatInfo.IsGroup == true  → 群聊（GroupChat API）
//   - ChatInfo.IsGroup == false → 单聊（SingleChat API）
func (s *qqSender) Send(ctx stdctx.Context, msg platform.OutboundMessage) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}

	chat, ok := platform.ChatInfoFromContext(ctx)
	if !ok {
		return errutil.ErrNoChatInfo
	}

	dtoMsg := buildDTOMessage(msg)

	// 频道消息（EventKindGuildMessage）需要独立的 Guild Channel API，暂不支持
	if chat.ParentID != "" {
		return fmt.Errorf("qq sender: guild channel message sending is not yet supported (guild_id=%s, channel_id=%s)", chat.ParentID, chat.ID)
	}

	if chat.IsGroup {
		_, err := s.api.GroupChat(chat.ID, dtoMsg)
		return err
	}
	_, err := s.api.SingleChat(chat.ID, dtoMsg)
	return err
}

// buildDTOMessage 将 platform.OutboundMessage 转换为 dto.Message
func buildDTOMessage(msg platform.OutboundMessage) *dto.Message {
	dtoMsg := &dto.Message{}

	// 优先使用 Markdown，其次 Text
	if msg.Markdown != "" {
		dtoMsg.Type = dto.MarkdownMessage
		dtoMsg.Markdown = &dto.Markdown{Content: msg.Markdown}
	} else {
		dtoMsg.Type = dto.TextMessage
		dtoMsg.Content = msg.Text
	}

	// 处理 Mentions（@ 用户）：将用户 ID 列表转换为 QQ AT 标签，前置于正文
	if len(msg.Mentions) > 0 {
		var sb strings.Builder
		for _, uid := range msg.Mentions {
			sb.WriteString(dto.At(uid))
		}
		if dtoMsg.Type == dto.MarkdownMessage && dtoMsg.Markdown != nil {
			dtoMsg.Markdown.Content = sb.String() + dtoMsg.Markdown.Content
		} else {
			dtoMsg.Content = sb.String() + dtoMsg.Content
		}
	}

	// 回复消息 ID
	if msg.ReplyToID != "" {
		dtoMsg.MessageID = dto.EventID(msg.ReplyToID)
	}

	// 扩展字段
	if v, ok := msg.Extra["msg_seq"]; ok {
		if seq, ok2 := v.(uint64); ok2 {
			dtoMsg.MessageSeq = seq
		}
	}
	if v, ok := msg.Extra["event_id"]; ok {
		if eid, ok2 := v.(string); ok2 {
			dtoMsg.EventID = dto.EventID(eid)
		}
	}

	return dtoMsg
}

// QQCapabilities 是 QQ 平台的能力声明
var QQCapabilities = platform.Capabilities{
	Markdown:        true,
	Buttons:         true,
	MultiAttachment: false,
	MessageEdit:     false,
	MessageDelete:   false,
	Embeds:          false,
	FileUpload:      true,
	GuildSupport:    true,
}
