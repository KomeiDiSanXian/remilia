package qq

import (
	stdctx "context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// qqSender 将 platform.Sender 接口桥接到 openapi.OpenAPI
type qqSender struct {
	api    openapi.OpenAPI
	msgSeq atomic.Uint64 // 自增消息序列号，QQ v2 API 防重放要求；手动 ApplyExtra 可覆盖
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
// 目标会话信息从 req.Target 读取（ChatInfo 类型，包含 ID、IsGroup 等路由字段）。
// 若 req.Target.ID 为空，返回 errutil.ErrNoChatInfo。
//
// req.EventID 为触发事件 ID，用于被动回复自动关联，框架通过 ctx.Reply 自动填充。
// 若 MessageExtra.EventID（通过 ApplyExtra 手动注入）非空，则优先使用手动值。
func (s *qqSender) Send(ctx stdctx.Context, req platform.SendRequest) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}

	chat := req.Target
	if chat.ID == "" {
		return errutil.ErrNoChatInfo
	}

	msg := req.Message

	// 频道消息（ChatInfo.ParentID 非空）使用频道专属 API
	if chat.ParentID != "" {
		return s.sendGuildChannelMessage(ctx, chat, req.EventID, msg)
	}

	// 富媒体优先（QQ 不支持多附件，取第一个）
	if len(msg.Attachments) > 0 {
		return s.sendAttachment(ctx, chat, req.EventID, msg, msg.Attachments[0])
	}

	dtoMsg := s.buildDTOMessage(msg, req.EventID)
	if chat.IsGroup {
		_, err := s.api.GroupChat(ctx, chat.ID, dtoMsg)
		return err
	}
	_, err := s.api.SingleChat(ctx, chat.ID, dtoMsg)
	return err
}

// sendAttachment 实现 QQ 富媒体两步发送：先上传获取 file_info，再发送 MediaMessage。
func (s *qqSender) sendAttachment(ctx stdctx.Context, chat platform.ChatInfo, eventID string, msg platform.OutboundMessage, att platform.Attachment) error {
	if att.URL == "" && len(att.Data) == 0 {
		return fmt.Errorf("qq sender: attachment has neither URL nor data")
	}

	media := &dto.Media{
		Type:       attachmentKindToFileType(att.Kind),
		ActiveSend: false,
	}
	if len(att.Data) > 0 {
		// 二进制数据通过 base64 编码后以 file_data 字段上传
		media.FileData = base64.StdEncoding.EncodeToString(att.Data)
	} else {
		media.URL = att.URL
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
	dtoMsg := s.buildDTOMessage(msg, eventID)
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

// sendGuildChannelMessage 向 QQ 文字子频道或频道私信发送消息。
//
// 路由规则：
//   - chat.IsDM = true → 频道私信，使用 DMChat（POST /dms/{guild_id}/messages）
//   - 其他 → 文字子频道，使用 ChannelChat（POST /channels/{channel_id}/messages）
//
// 频道私信时 chat.ID 存储的是 DM 会话的 guild_id（由 NewEvent/populateGuildMessage 填充）。
// chat.ID = channel_id（文字子频道）或 guild_id（私信会话），chat.ParentID = guild_id。
func (s *qqSender) sendGuildChannelMessage(ctx stdctx.Context, chat platform.ChatInfo, eventID string, msg platform.OutboundMessage) error {
	guildMsg := s.buildGuildDTOMessage(msg, eventID)
	if chat.IsDM {
		// 频道私信：chat.ID 为 DM 会话的 guild_id
		_, err := s.api.DMChat(ctx, chat.ID, guildMsg)
		return err
	}
	_, err := s.api.ChannelChat(ctx, chat.ID, guildMsg)
	return err
}

// buildGuildDTOMessage 将 platform.OutboundMessage 转换为频道专属的 dto.GuildMessage。
//
// 优先级：Markdown > Text(Content) > Image(Attachment)
// 被动消息：MsgID 优先使用 MessageExtra.EventID，其次使用 req.EventID。
// 引用回复：ReplyToID 非空时设置 MessageReference（展示被引用消息气泡）。
func (s *qqSender) buildGuildDTOMessage(msg platform.OutboundMessage, eventID string) *dto.GuildMessage {
	guildMsg := &dto.GuildMessage{}

	// 消息内容优先级：Markdown > 纯文本
	if msg.Markdown != "" {
		guildMsg.Markdown = &dto.Markdown{Content: msg.Markdown}
	} else {
		guildMsg.Content = msg.Text
	}

	// @用户：将 Mentions 转为 QQ AT 内嵌标签，前置于正文
	if len(msg.Mentions) > 0 {
		var sb strings.Builder
		for _, uid := range msg.Mentions {
			sb.WriteString(dto.At(uid))
		}
		if guildMsg.Markdown != nil {
			guildMsg.Markdown.Content = sb.String() + guildMsg.Markdown.Content
		} else {
			guildMsg.Content = sb.String() + guildMsg.Content
		}
	}

	// 图片附件：频道 API 直接接受图片 URL，无需预先上传
	// 非图片类型（音视频/文件）在频道消息中不支持，忽略
	if len(msg.Attachments) > 0 {
		att := msg.Attachments[0]
		if att.Kind == platform.AttachmentKindImage && att.URL != "" {
			guildMsg.Image = att.URL
		}
	}

	// 被动消息关联：频道用 msg_id（来源消息的 Message.id）
	// 优先使用手动 ApplyExtra 注入的值，其次 SendRequest.EventID
	extra := extractExtra(msg)
	resolvedMsgID := extra.EventID
	if resolvedMsgID == "" {
		resolvedMsgID = eventID
	}
	if resolvedMsgID != "" {
		guildMsg.MsgID = resolvedMsgID
	}

	// 引用回复：展示消息气泡引用（不同于被动回复关联）
	if msg.ReplyToID != "" {
		guildMsg.MessageReference = &dto.MessageReference{
			MessageID:             msg.ReplyToID,
			IgnoreGetMessageError: true,
		}
	}

	return guildMsg
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
// eventID 优先使用 MessageExtra.EventID（手动 ApplyExtra，优先级最高）；
// 若为空，则使用传入的 eventID（由 SendRequest.EventID 自动携带，来自 ctx.Reply）。
func (s *qqSender) buildDTOMessage(msg platform.OutboundMessage, eventID string) *dto.Message {
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
	} else {
		// 自动递增序列号，确保每条消息序号唯一（QQ v2 API 防重放要求）
		dtoMsg.MessageSeq = s.msgSeq.Add(1)
	}

	// D4：EventID 优先使用手动注入的值（ApplyExtra）；若为空，使用来自 SendRequest 的值
	resolvedEventID := extra.EventID
	if resolvedEventID == "" {
		resolvedEventID = eventID
	}
	if resolvedEventID != "" {
		dtoMsg.EventID = dto.EventID(resolvedEventID)
	}

	// IsWakeup：互动召回消息，与 event_id/msg_id 互斥，仅在 extra 中显式开启时有效
	if extra.IsWakeup {
		dtoMsg.IsWakeup = true
		// 召回消息不关联来源事件/消息，清除已设置的 ID 字段
		dtoMsg.EventID = ""
		dtoMsg.MessageID = ""
	}

	return dtoMsg
}

// Capabilities 是 QQ 平台的能力声明
var Capabilities = platform.Capabilities{
	Markdown:        true,
	Buttons:         true,
	MultiAttachment: false,
	MessageEdit:     false,
	MessageDelete:   false,
	Embeds:          false,
	FileUpload:      true,
	GuildSupport:    true,
	Reactions:       true,
	ThreadReply:     true,
	TypingIndicator: false,
	MentionAll:      true,
	VoiceChannel:    false,
}
