package satori

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ─────────────────────────────────────────────────────────────────────────────
// satoriSender – 实现 platform.Sender 及可选接口
// ─────────────────────────────────────────────────────────────────────────────

// satoriSender 是 Satori 协议的消息发送器。
//
// 实现了以下接口：
//   - platform.Sender         （message.create）
//   - platform.MessageEditor  （message.update）
//   - platform.MessageDeleter （message.delete）
//   - platform.ReactionSender （reaction.create / reaction.delete）
type satoriSender struct {
	client *Client
}

func newSender(client *Client) *satoriSender {
	return &satoriSender{client: client}
}

// ─────────────────────────────────────────────────────────────────────────────
// platform.Sender
// ─────────────────────────────────────────────────────────────────────────────

// Send 将 msg 编码为 Satori XML 消息内容字符串并调用 message.create 发送。
//
// 频道 ID 取自 req.Target.ID。
// 若平台因内容包含 <message> 元素而返回多个 Message 对象，SendResult 仅反映第一个。
//
// 被动请求支持（实验性）：若 req.Target.Tokens 中携带 TokenSatoriReferrer，
// 则将其作为 referrer 参数传入 message.create API，以满足对主动/被动操作加以
// 区分的平台（如 Lark）的要求。
// 参见：https://satori.chat/zh-CN/advanced/passive.html
func (s *satoriSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if err := req.Validate(); err != nil {
		return platform.SendResult{}, err
	}

	content := EncodeOutboundMessage(req.Message)

	createReq := MessageCreateRequest{
		ChannelID: req.Target.ID,
		Content:   content,
	}
	// 被动请求：将 referrer JSON 从 Tokens 还原为 *json.RawMessage
	if raw, ok := req.Target.Tokens[TokenSatoriReferrer]; ok && raw != "" {
		r := json.RawMessage(raw)
		createReq.Referrer = &r
	}

	msgs, err := s.client.MessageCreateWith(ctx, createReq)
	if err != nil {
		return platform.SendResult{}, fmt.Errorf("satori: 发送消息: %w", err)
	}

	result := platform.SendResult{Platform: s.client.platform}
	if len(msgs) > 0 && msgs[0] != nil {
		result.MessageID = msgs[0].ID
		if msgs[0].CreatedAt != nil {
			result.Timestamp = time.UnixMilli(*msgs[0].CreatedAt)
		}
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// platform.MessageEditor
// ─────────────────────────────────────────────────────────────────────────────

// Edit 通过 message.update 编辑已有消息。
func (s *satoriSender) Edit(ctx stdctx.Context, chatID, messageID string, msg platform.OutboundMessage) error {
	content := EncodeOutboundMessage(msg)
	if err := s.client.MessageUpdate(ctx, chatID, messageID, content); err != nil {
		return fmt.Errorf("satori: 编辑消息: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// platform.MessageDeleter
// ─────────────────────────────────────────────────────────────────────────────

// Delete 通过 message.delete 撤回/删除消息。
func (s *satoriSender) Delete(ctx stdctx.Context, chatID, messageID string) error {
	if err := s.client.MessageDelete(ctx, chatID, messageID); err != nil {
		return fmt.Errorf("satori: 删除消息: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// platform.ReactionSender
// ─────────────────────────────────────────────────────────────────────────────

// AddReaction 通过 reaction.create 为消息添加表态。
//
// emoji 值的解析规则：
//   - EmojiKindUnicode → emoji.Value（如 "👍"）
//   - EmojiKindCustom  → emoji.ID
//   - EmojiKindSystem  → emoji.ID
func (s *satoriSender) AddReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	emojiID := resolveEmojiID(emoji)
	if err := s.client.ReactionCreate(ctx, chatID, messageID, emojiID); err != nil {
		return fmt.Errorf("satori: 添加表态: %w", err)
	}
	return nil
}

// RemoveReaction 通过 reaction.delete 从消息中移除表态。
func (s *satoriSender) RemoveReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	emojiID := resolveEmojiID(emoji)
	if err := s.client.ReactionDelete(ctx, chatID, messageID, emojiID, ""); err != nil {
		return fmt.Errorf("satori: 移除表态: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 编译期接口断言
// ─────────────────────────────────────────────────────────────────────────────

var (
	_ platform.Sender         = (*satoriSender)(nil)
	_ platform.MessageEditor  = (*satoriSender)(nil)
	_ platform.MessageDeleter = (*satoriSender)(nil)
	_ platform.ReactionSender = (*satoriSender)(nil)
)

// ─────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// resolveEmojiID 将 platform.Emoji 转换为 Satori 表态 API 所需的字符串。
// Unicode 表情使用字符本身，自定义/系统表情使用 ID 字段。
func resolveEmojiID(e platform.Emoji) string {
	switch e.Kind {
	case platform.EmojiKindUnicode:
		return e.Value
	default:
		if e.ID != "" {
			return e.ID
		}
		return e.Value
	}
}
