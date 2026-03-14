package qq

import (
	stdctx "context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// qqSender 将 platform.Sender 接口桥接到 openapi.OpenAPI
type qqSender struct {
	api openapi.OpenAPI
}

// NewSender 创建 QQ 平台的消息发送器
func NewSender(api openapi.OpenAPI) platform.Sender {
	return &qqSender{api: api}
}

// Send 将 OutboundMessage 转换并发送到 QQ 平台
//
// 路由：从 platform.ChatInfo（由框架 Reply() 自动注入）读取会话类型。
// 若无 ChatInfo，Fallback 按群聊处理。
func (s *qqSender) Send(ctx stdctx.Context, chatID string, msg platform.OutboundMessage) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}

	dtoMsg := buildDTOMessage(msg)

	// 从 platform.ChatInfo（由框架 Reply 注入）读取会话类型
	if chat, ok := platform.ChatInfoFromContext(ctx); ok {
		if chat.IsGroup {
			_, err := s.api.GroupChat(chatID, dtoMsg)
			return err
		}
		_, err := s.api.SingleChat(chatID, dtoMsg)
		return err
	}

	// Fallback：无法判断类型时，尝试群聊（大多数场景）
	_, err := s.api.GroupChat(chatID, dtoMsg)
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
