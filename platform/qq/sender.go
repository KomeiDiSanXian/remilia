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
// 路由优先级：
//  1. platform.ChatInfoFromContext(ctx).IsGroup — 由框架 Reply() 自动注入（推荐）
//  2. ctx.Value(chatTypeContextKey{}) — 旧版手动注入方式（Deprecated）
//  3. Fallback：按群聊处理（大多数场景）
func (s *qqSender) Send(ctx stdctx.Context, chatID string, msg platform.OutboundMessage) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}

	dtoMsg := buildDTOMessage(msg)

	// 优先：从 platform.ChatInfo（由框架 Reply 注入）读取会话类型
	if chat, ok := platform.ChatInfoFromContext(ctx); ok {
		if chat.IsGroup {
			_, err := s.api.GroupChat(chatID, dtoMsg)
			return err
		}
		_, err := s.api.SingleChat(chatID, dtoMsg)
		return err
	}

	// 降级：旧版 chatTypeContextKey（手动注入，向后兼容）
	if ct, ok := ctx.Value(chatTypeContextKey{}).(chatType); ok {
		switch ct {
		case chatTypeGroup:
			_, err := s.api.GroupChat(chatID, dtoMsg)
			return err
		case chatTypePrivate:
			_, err := s.api.SingleChat(chatID, dtoMsg)
			return err
		}
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

// chatType 标记会话类型（旧版手动注入，已由 platform.ChatInfo 取代）
//
// Deprecated: 框架内部现通过 platform.WithChatInfo 自动注入 ChatInfo，
// 无需手动维护 chatType。此类型仅保留以向后兼容旧代码。
type chatType int

const (
	chatTypeUnknown chatType = iota
	chatTypePrivate
	chatTypeGroup
)

// chatTypeContextKey 是旧版注入 chatType 的 context key
//
// Deprecated: 使用 platform.WithChatInfo / platform.ChatInfoFromContext。
type chatTypeContextKey struct{}
