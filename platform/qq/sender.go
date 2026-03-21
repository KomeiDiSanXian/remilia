package qq

import (
	stdctx "context"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
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
// 路由规则：
//   - ChatInfo.ParentID 非空 → 频道消息，暂不支持，返回明确错误
//   - Attachments 非空（取第一个）→ 两步富媒体发送（上传后发送）
//   - 其余 → Text / Markdown 文本消息
//
// 目标会话信息从 ctx 的 ChatInfo 读取（由 Reply 或 platform.WithChatInfo 注入）。
// 若 ctx 未携带 ChatInfo，返回 errutil.ErrNoChatInfo。
//
// D4：若 MessageExtra.EventID 为空，自动从 ctx 读取触发事件 ID（由 ctx.Reply 注入），
// 无需手动调用 qq.ApplyExtra。手动 ApplyExtra 仍然生效（优先级更高）。
func (s *qqSender) Send(ctx stdctx.Context, msg platform.OutboundMessage) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}

	chat, ok := platform.ChatInfoFromContext(ctx)
	if !ok {
		return errutil.ErrNoChatInfo
	}

	// 频道消息（ChatInfo.ParentID 非空）需要独立的 Guild Channel API，暂不支持
	if chat.ParentID != "" {
		return fmt.Errorf("qq sender: guild channel message sending is not yet supported (guild_id=%s, channel_id=%s)", chat.ParentID, chat.ID)
	}

	// 富媒体优先（QQ 不支持多附件，取第一个）
	if len(msg.Attachments) > 0 {
		return s.sendAttachment(ctx, chat, msg, msg.Attachments[0])
	}

	dtoMsg := s.buildDTOMessage(ctx, msg)
	if chat.IsGroup {
		_, err := s.api.GroupChat(ctx, chat.ID, dtoMsg)
		return err
	}
	_, err := s.api.SingleChat(ctx, chat.ID, dtoMsg)
	return err
}

// sendAttachment 实现 QQ 富媒体两步发送：先上传获取 file_info，再发送 MediaMessage。
func (s *qqSender) sendAttachment(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage, att platform.Attachment) error {
	if att.URL == "" && len(att.Data) == 0 {
		return fmt.Errorf("qq sender: attachment has neither URL nor data")
	}
	if len(att.Data) > 0 {
		return fmt.Errorf("qq sender: binary attachment upload is not yet supported; use URL attachment instead")
	}

	media := &dto.Media{
		Type:       attachmentKindToFileType(att.Kind),
		URL:        att.URL,
		ActiveSend: false,
	}

	var (
		uploadResult gjson.Result
		err          error
	)
	if chat.IsGroup {
		uploadResult, err = s.api.GroupRichMedia(ctx, chat.ID, media)
	} else {
		uploadResult, err = s.api.SingleRichMedia(ctx, chat.ID, media)
	}
	if err != nil {
		return fmt.Errorf("qq sender: media upload failed: %w", err)
	}

	fileInfo := uploadResult.Get("file_info").String()
	if fileInfo == "" {
		return fmt.Errorf("qq sender: media upload returned empty file_info (response: %s)", uploadResult.Raw)
	}

	// 构建携带 file_info 的 MediaMessage
	dtoMsg := s.buildDTOMessage(ctx, msg)
	dtoMsg.Type = dto.MediaMessage
	dtoMsg.Media = &dto.MediaResponse{FileInfo: fileInfo}
	dtoMsg.Content = "" // 媒体消息不携带文本内容

	if chat.IsGroup {
		_, err = s.api.GroupChat(ctx, chat.ID, dtoMsg)
	} else {
		_, err = s.api.SingleChat(ctx, chat.ID, dtoMsg)
	}
	return err
}

// attachmentKindToFileType 将平台无关的附件类型映射到 QQ dto.FileType
func attachmentKindToFileType(kind platform.AttachmentKind) dto.FileType {
	switch kind {
	case platform.AttachmentKindImage:
		return dto.ImageFile
	case platform.AttachmentKindVideo:
		return dto.VideoFile
	case platform.AttachmentKindAudio:
		return dto.AudioFile
	default:
		return dto.File
	}
}

// buildDTOMessage 将 platform.OutboundMessage 转换为 dto.Message。
//
// D4：若 MessageExtra.EventID 为空，自动从 ctx（由 platform.WithEventID 注入）
// 读取触发事件 ID，实现被动回复自动关联，无需手动 ApplyExtra。
func (s *qqSender) buildDTOMessage(ctx stdctx.Context, msg platform.OutboundMessage) *dto.Message {
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

	// 提取 QQ 专属参数（手动 ApplyExtra 优先级最高）
	extra := extractExtra(msg)
	if extra.MsgSeq != 0 {
		dtoMsg.MessageSeq = extra.MsgSeq
	}

	// D4：EventID 优先使用手动注入的值；若为空，从 context 自动读取
	eventID := extra.EventID
	if eventID == "" {
		if id, ok := platform.EventIDFromContext(ctx); ok {
			eventID = id
		}
	}
	if eventID != "" {
		dtoMsg.EventID = dto.EventID(eventID)
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
