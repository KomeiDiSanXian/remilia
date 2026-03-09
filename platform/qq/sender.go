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
// 根据 chatID 所属的会话类型（私聊/群聊），自动路由到对应 API。
// 目前通过 ctx.Value(chatTypeKey) 判断会话类型（见 adapter.go 注入逻辑）；
// 若未注入则按 chatID 前缀启发式判断（fallback）。
func (s *qqSender) Send(ctx stdctx.Context, chatID string, msg platform.OutboundMessage) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}

	dtoMsg := buildDTOMessage(msg)

	// 从 context 中取出会话类型（由 adapter 注入）
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

// chatType 标记会话类型，注入到 context 中供 sender 路由
type chatType int

const (
	chatTypeUnknown chatType = iota
	chatTypePrivate
	chatTypeGroup
)

// chatTypeContextKey 是注入 chatType 的 context key
type chatTypeContextKey struct{}
