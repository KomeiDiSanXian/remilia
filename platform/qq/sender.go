package qq

import (
	stdctx "context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

const (
	// maxPassiveReplies 每个消息最多被动回复次数（QQ 平台限制）
	maxPassiveReplies = 5
	// passiveReplyTTL 被动回复有效时长（QQ 平台限制）
	passiveReplyTTL = 5 * time.Minute
)

// msgSeqEntry 按 msg_id 跟踪被动回复状态。
type msgSeqEntry struct {
	seq       atomic.Uint64
	count     atomic.Uint64
	createdAt atomic.Value // time.Time
}

// qqSender 将 platform.Sender 接口桥接到 openapi.OpenAPI
type qqSender struct {
	api       openapi.OpenAPI
	msgSeqMap sync.Map // map[string]*msgSeqEntry，按 msg_id 管理回复状态
}

// NewSender 创建 QQ 平台的消息发送器
func NewSender(api openapi.OpenAPI) platform.Sender {
	return &qqSender{api: api}
}

// Send 将 OutboundMessage 转换并发送到 QQ 平台，返回平台响应摘要。
//
// 路由规则：
//   - ChatInfo.ParentID 非空 → 频道消息（子频道 / 频道私信）
//   - Attachments 非空（取第一个）→ 两步富媒体发送（上传 → 发送）
//   - 其余 → Text / Markdown 文本消息
//
// SendResult.Raw 类型为 *SendResult，包含完整的 QQ 平台响应字段。
// 富媒体两步发送时，上传阶段（FileInfo、FileUUID、TTL）与发送阶段（MessageID）
// 均合并在同一个 *SendResult 中返回。
func (s *qqSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if s.api == nil {
		return platform.SendResult{}, fmt.Errorf("qq sender: openAPI client is nil")
	}

	chat := req.Target
	if chat.ID == "" {
		return platform.SendResult{}, errutil.ErrNoChatInfo
	}

	msg := req.Message

	// 被动回复限频检查（5 分钟、5 次上限）
	if msgID := resolveMsgID(msg, chat); msgID != "" {
		if err := s.checkReplyLimit(msgID); err != nil {
			return platform.SendResult{}, err
		}
	}

	// 频道消息（ChatInfo.ParentID 非空）使用频道专属 API
	if chat.ParentID != "" {
		return s.sendGuildChannelMessage(ctx, chat, msg)
	}

	// 富媒体优先（QQ 不支持多附件，取第一个）
	if len(msg.Attachments) > 0 {
		return s.sendAttachment(ctx, chat, msg, msg.Attachments[0])
	}

	dtoMsg := s.buildDTOMessage(msg, chat)
	var (
		raw gjson.Result
		err error
	)
	if chat.IsGroup {
		raw, err = s.api.GroupChat(ctx, chat.ID, dtoMsg)
	} else {
		raw, err = s.api.SingleChat(ctx, chat.ID, dtoMsg)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, "qq", chat.ID,
			err.Error(), 0, err,
		)
	}
	return buildSendResult(raw), nil
}

// sendAttachment 实现 QQ 富媒体两步发送：先上传获取 file_info，再发送 MediaMessage。
// 返回的 SendResult.Raw(*SendResult) 同时包含上传响应和发送响应的字段。
func (s *qqSender) sendAttachment(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage, att platform.Attachment) (platform.SendResult, error) {
	if att.URL == "" && len(att.Data) == 0 {
		return platform.SendResult{}, fmt.Errorf("qq sender: attachment has neither URL nor data")
	}

	media := &dto.Media{
		Type:       attachmentKindToFileType(att.Kind),
		ActiveSend: false,
	}
	if len(att.Data) > 0 {
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
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrNetworkError, "qq", chat.ID,
			fmt.Sprintf("media upload failed: %v", err), 0, err,
		)
	}

	fileInfo := uploadResult.Get("file_info").String()
	if fileInfo == "" {
		return platform.SendResult{}, fmt.Errorf("qq sender: media upload returned empty file_info (response: %s)", uploadResult.Raw)
	}

	// 构建携带 file_info 的 MediaMessage
	dtoMsg := s.buildDTOMessage(msg, chat)
	dtoMsg.Type = dto.MediaMessage
	dtoMsg.Media = &dto.MediaResponse{FileInfo: fileInfo}
	// 群聊接口 content 字段为必填（API 文档标注"是"），不可清空。
	// 单聊接口 content 为可选，媒体消息通常不携带文本内容。
	if !chat.IsGroup {
		dtoMsg.Content = ""
	} else if dtoMsg.Content == "" {
		dtoMsg.Content = " "
	}

	var sendResult gjson.Result
	if chat.IsGroup {
		sendResult, err = s.api.GroupChat(ctx, chat.ID, dtoMsg)
	} else {
		sendResult, err = s.api.SingleChat(ctx, chat.ID, dtoMsg)
	}
	if err != nil {
		return platform.SendResult{}, err
	}

	// 合并上传响应与发送响应
	return buildSendResultFromUpload(uploadResult, sendResult), nil
}

// sendGuildChannelMessage 向 QQ 文字子频道或频道私信发送消息。
func (s *qqSender) sendGuildChannelMessage(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage) (platform.SendResult, error) {
	guildMsg := s.buildGuildDTOMessage(msg, chat)
	var (
		raw gjson.Result
		err error
	)
	if chat.IsDM {
		raw, err = s.api.DMChat(ctx, chat.ID, guildMsg)
	} else {
		raw, err = s.api.ChannelChat(ctx, chat.ID, guildMsg)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, "qq", chat.ID,
			err.Error(), 0, err,
		)
	}
	return buildSendResult(raw), nil
}

// buildSendResult 从普通发送响应构建 platform.SendResult。
func buildSendResult(raw gjson.Result) platform.SendResult {
	qqResult := &SendResult{
		MessageID: raw.Get("id").String(),
	}
	if ts := raw.Get("timestamp").Int(); ts > 0 {
		qqResult.Timestamp = time.Unix(ts, 0)
	}
	return platform.SendResult{
		MessageID: qqResult.MessageID,
		Timestamp: qqResult.Timestamp,
		Platform:  "qq",
		Raw:       qqResult,
	}
}

// buildSendResultFromUpload 合并富媒体上传响应与消息发送响应，构建 platform.SendResult。
func buildSendResultFromUpload(uploadRaw, sendRaw gjson.Result) platform.SendResult {
	qqResult := &SendResult{
		// 来自发送响应
		MessageID: sendRaw.Get("id").String(),
		// 来自上传响应
		FileUUID: uploadRaw.Get("file_uuid").String(),
		FileInfo: uploadRaw.Get("file_info").String(),
		TTL:      int(uploadRaw.Get("ttl").Int()),
	}
	if ts := sendRaw.Get("timestamp").Int(); ts > 0 {
		qqResult.Timestamp = time.Unix(ts, 0)
	}
	return platform.SendResult{
		MessageID: qqResult.MessageID,
		Timestamp: qqResult.Timestamp,
		Platform:  "qq",
		Raw:       qqResult,
	}
}

// buildGuildDTOMessage 将 platform.OutboundMessage 转换为频道专属的 dto.GuildMessage。
//
// 优先级：Ark > Markdown > Text(Content) > Image(Attachment)
// 被动消息：MsgID 优先使用 MessageExtra.EventID，其次使用 req.EventID。
// 引用回复：ReplyToID 非空时设置 MessageReference（展示被引用消息气泡）。
func (s *qqSender) buildGuildDTOMessage(msg platform.OutboundMessage, chat platform.ChatInfo) *dto.GuildMessage {
	guildMsg := &dto.GuildMessage{}
	extra := extractExtra(msg)

	// 消息类型优先级：Ark > Markdown > Text
	if extra.Ark != nil {
		guildMsg.Ark = convertArk(extra.Ark)
	} else if msg.Markdown != "" {
		guildMsg.Markdown = &dto.Markdown{Content: msg.Markdown}
	} else {
		guildMsg.Content = msg.Text
	}

	// @用户：将 Mentions 转为 QQ AT 内嵌标签，前置于正文
	if len(msg.Mentions) > 0 && extra.Ark == nil {
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

	// 交互按钮：转换为 InlineKeyboard
	if len(msg.Buttons) > 0 {
		guildMsg.Keyboard = dto.MarshalKeyboard(convertButtons(msg.Buttons))
	}

	// 被动消息关联：频道用 msg_id（来源消息的 Message.id / payload.ID）
	// 优先使用手动 ApplyExtra 注入的值（extra.EventID），其次 ChatInfo.Tokens[TokenMsgID]
	resolvedMsgID := extra.EventID
	if resolvedMsgID == "" {
		resolvedMsgID = chat.Tokens[TokenMsgID] // 频道消息：payload.ID 即 message id
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

// buildDTOMessage 将 platform.OutboundMessage 转换为 dto.Message（用于 C2C / 群聊）。
//
// 被动回复 ID 优先级：
//   - msg_id：msg.ReplyToID（手动设置）> chat.Tokens[TokenMsgID]（C2C_MESSAGE_CREATE / GROUP_AT_MESSAGE_CREATE 自动填充）
//   - event_id：extra.EventID（手动 ApplyExtra）> chat.Tokens[TokenEventID]（INTERACTION_CREATE / C2C_MSG_RECEIVE 等自动填充）
//
// 主动消息：ChatInfo.Tokens 中无相应 token 时，不设置 msg_id / event_id，即为主动消息。
func (s *qqSender) buildDTOMessage(msg platform.OutboundMessage, chat platform.ChatInfo) *dto.Message {
	dtoMsg := &dto.Message{}
	extra := extractExtra(msg)

	// 消息类型优先级：Ark > Markdown > Text
	if extra.Ark != nil {
		dtoMsg.Type = dto.ArkMessage
		dtoMsg.Ark = convertArk(extra.Ark)
	} else if msg.Markdown != "" {
		dtoMsg.Type = dto.MarkdownMessage
		dtoMsg.Markdown = &dto.Markdown{Content: msg.Markdown}
	} else {
		dtoMsg.Type = dto.TextMessage
		dtoMsg.Content = msg.Text
	}

	// 处理 Mentions（@ 用户）：将用户 ID 列表转换为 QQ AT 标签，前置于正文
	if len(msg.Mentions) > 0 && extra.Ark == nil {
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

	// 交互按钮：转换为 InlineKeyboard（QQ 按钮需附在 Markdown 消息上）
	if len(msg.Buttons) > 0 && extra.Ark == nil {
		dtoMsg.Keyboard = dto.MarshalKeyboard(convertButtons(msg.Buttons))
	}

	// 回复消息 ID（msg_id）：被动回复授权 token（message-based）
	// 优先级：msg.ReplyToID（手动设置）> chat.Tokens[TokenMsgID]（框架从 C2C/Group 消息事件自动填充）
	resolvedMsgID := msg.ReplyToID
	if resolvedMsgID == "" {
		resolvedMsgID = chat.Tokens[TokenMsgID]
	}
	if resolvedMsgID != "" {
		dtoMsg.MessageID = dto.EventID(resolvedMsgID)
	}

	if extra.MsgSeq != 0 {
		dtoMsg.MessageSeq = extra.MsgSeq
	} else {
		// 按 msg_id 递增序列号，避免相同 msg_id 重复发送
		// 空 msg_id 时设为 0 表示不设置此字段
		dtoMsg.MessageSeq = s.nextMsgSeq(string(dtoMsg.MessageID))
	}

	// event_id：被动回复授权 token（event-based，仅 INTERACTION_CREATE / C2C_MSG_RECEIVE 等）
	// 优先级：extra.EventID（手动 ApplyExtra）> chat.Tokens[TokenEventID]（框架从事件类型自动填充）
	resolvedEventID := extra.EventID
	if resolvedEventID == "" {
		resolvedEventID = chat.Tokens[TokenEventID]
	}
	if resolvedEventID != "" {
		dtoMsg.EventID = dto.EventID(resolvedEventID)
	}

	// IsWakeup：互动召回消息，与 event_id/msg_id 互斥，仅在 extra 中显式开启时有效。
	// 注意：is_wakeup 字段仅 QQ 单聊（C2C）接口支持，群聊接口不存在此字段。
	if extra.IsWakeup && !chat.IsGroup {
		dtoMsg.IsWakeup = true
		// 召回消息不关联来源事件/消息，清除已设置的 ID 字段
		dtoMsg.EventID = ""
		dtoMsg.MessageID = ""
	}

	return dtoMsg
}

// resolveMsgID 从消息和会话信息中解析出用于被动回复的 msg_id。
// 空字符串表示主动消息，不受被动回复限制。
func resolveMsgID(msg platform.OutboundMessage, chat platform.ChatInfo) string {
	id := msg.ReplyToID
	if id == "" {
		id = chat.Tokens[TokenMsgID]
	}
	return id
}

// nextMsgSeq 返回指定 msg_id 的下一个消息序列号。
//
// msg_seq 是 QQ v2 API 防重放字段，与 msg_id 联合使用：
// 相同的 msg_id + msg_seq 重复发送会失败。
// 不填默认是 1，框架按 msg_id 分别递增。
// msgID 为空时（主动消息），返回 0 表示不设置 msg_seq。
func (s *qqSender) nextMsgSeq(msgID string) uint64 {
	if msgID == "" {
		return 0
	}
	v, _ := s.msgSeqMap.LoadOrStore(msgID, &msgSeqEntry{})
	entry := v.(*msgSeqEntry)
	entry.count.Add(1)
	if entry.createdAt.Load() == nil {
		entry.createdAt.Store(time.Now())
	}
	return entry.seq.Add(1)
}

// checkReplyLimit 检查对指定 msg_id 的被动回复是否超限。
//
// QQ 平台限制：单条消息最多回复 5 次，有效时长 5 分钟。
// 返回 nil 表示允许发送；返回 error 表示已达上限。
// msgID 为空时（主动消息）直接返回 nil。
func (s *qqSender) checkReplyLimit(msgID string) error {
	if msgID == "" {
		return nil
	}
	v, ok := s.msgSeqMap.Load(msgID)
	if !ok {
		return nil // 首次回复，加载会在 nextMsgSeq 中完成
	}
	entry := v.(*msgSeqEntry)

	// 检查 5 分钟有效期
	if created, ok := entry.createdAt.Load().(time.Time); ok && time.Since(created) > passiveReplyTTL {
		s.msgSeqMap.Delete(msgID)
		return fmt.Errorf("passive reply expired: msg_id=%q, created=%v, ttl=%v: %w",
			msgID, created, passiveReplyTTL, errutil.ErrPassiveReplyExpired)
	}

	// 检查回复次数上限
	if entry.count.Load() >= maxPassiveReplies {
		return fmt.Errorf("passive reply limit reached: msg_id=%q, count=%d, max=%d: %w",
			msgID, entry.count.Load(), maxPassiveReplies, errutil.ErrPassiveReplyLimitReached)
	}
	return nil
}

// Delete 撤回/删除消息。
//
// chatID 为目标会话 ID（openid / group_openid / channel_id）。
// 实现优先级：群聊 > 单聊 > 频道，以适应不同的 chatID 类型。
func (s *qqSender) Delete(ctx stdctx.Context, chatID, messageID string) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	var err error
	_, err = s.api.GroupReset(ctx, chatID, messageID)
	if err == nil {
		return nil
	}
	_, err = s.api.SingleReset(ctx, chatID, messageID)
	if err == nil {
		return nil
	}
	_, err = s.api.ChannelReset(ctx, chatID, messageID, true)
	return err
}

// AddReaction 为频道消息添加表情表态。
//
// 仅频道（Guild）消息支持，C2C 和群聊不支持。
// emoji.Kind 映射规则：
//   - EmojiKindSystem → emojiType=1, emojiID=emoji.ID（QQ 系统表情）
//   - EmojiKindUnicode → emojiType=2, emojiID=emoji.Value
//   - EmojiKindCustom → emojiType=2, emojiID=emoji.ID
func (s *qqSender) AddReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	emojiType, emojiID := resolveQQEmoji(emoji)
	_, err := s.api.AddReaction(ctx, chatID, messageID, emojiType, emojiID)
	return err
}

// RemoveReaction 移除频道消息的表情表态。
func (s *qqSender) RemoveReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	emojiType, emojiID := resolveQQEmoji(emoji)
	_, err := s.api.DeleteReaction(ctx, chatID, messageID, emojiType, emojiID)
	return err
}

// resolveQQEmoji 将 platform.Emoji 转换为 QQ 表态 API 的 emojiType 和 emojiID。
func resolveQQEmoji(emoji platform.Emoji) (emojiType int, emojiID string) {
	switch emoji.Kind {
	case platform.EmojiKindSystem:
		return 1, emoji.ID
	case platform.EmojiKindCustom:
		if emoji.ID != "" {
			return 2, emoji.ID
		}
		return 2, emoji.Value
	default: // EmojiKindUnicode
		return 2, emoji.Value
	}
}

// qqCapabilities 返回 QQ 平台的能力声明。
//
// 使用函数而非包级变量，保留运行时动态更新的能力（如连接后更新权限）。
func qqCapabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        true,
		Buttons:         true,
		MultiAttachment: false,
		MessageEdit:     false,
		MessageDelete:   true,
		Embeds:          false,
		FileUpload:      true,
		GuildSupport:    true,
		Reactions:       true,
		ThreadReply:     true,
		TypingIndicator: false,
		MentionAll:      true,
		VoiceChannel:    false,
		// QQ 按钮布局限制：最多 5 行，每行最多 5 个
		MaxButtonsPerRow: 5,
		MaxButtonRows:    5,
	}
}

// convertButtons 将平台无关的 []platform.Button 转换为 QQ InlineKeyboard。
//
// 行分组规则：
//   - Button.Row > 0：Row 值相同的按钮分入同一行；
//   - Button.Row == 0：每个按钮独占一行（安全默认值）。
//
// 最多 5 行，每行最多 5 个按钮（超出部分截断）。
// 按钮样式映射：ButtonStylePrimary → style=1（蓝色），其余 → style=0（灰色）。
// 按钮操作映射：ButtonStyleLink + URL → type=0（跳转），其余 → type=1（回调）。
func convertButtons(buttons []platform.Button) *dto.InlineKeyboard {
	const maxRows, maxPerRow = 5, 5

	keyOrder := make([]int, 0, len(buttons))
	rowMap := make(map[int][]platform.Button)
	autoKey := 0 // 从 -1 递减，给 Row=0 的按钮分配唯一 key

	for _, b := range buttons {
		if b.Row == 0 {
			autoKey--
			rowMap[autoKey] = []platform.Button{b}
			keyOrder = append(keyOrder, autoKey)
		} else {
			if _, exists := rowMap[b.Row]; !exists {
				keyOrder = append(keyOrder, b.Row)
			}
			rowMap[b.Row] = append(rowMap[b.Row], b)
		}
	}

	rows := make([]dto.KeyboardRow, 0, min(len(keyOrder), maxRows))
	for _, key := range keyOrder {
		if len(rows) >= maxRows {
			break
		}
		btns := rowMap[key]
		if len(btns) > maxPerRow {
			btns = btns[:maxPerRow]
		}
		kbBtns := make([]dto.KeyboardButton, 0, len(btns))
		for _, b := range btns {
			style := 0
			if b.Style == platform.ButtonStylePrimary {
				style = 1
			}
			actionType := 1 // 默认回调
			data := b.ID
			if b.Style == platform.ButtonStyleLink && b.URL != "" {
				actionType = 0 // 跳转
				data = b.URL
			}
			kbBtns = append(kbBtns, dto.KeyboardButton{
				ID: b.ID,
				RenderData: &dto.KeyboardRenderData{
					Label:        b.Label,
					VisitedLabel: b.Label,
					Style:        style,
				},
				Action: &dto.KeyboardAction{
					Type: actionType,
					Permission: &dto.KeyboardPermission{
						Type: 2, // 所有人可操作
					},
					Data:          data,
					UnsupportTips: "当前版本不支持此操作",
				},
			})
		}
		rows = append(rows, dto.KeyboardRow{Buttons: kbBtns})
	}

	return &dto.InlineKeyboard{
		Content: &dto.InlineKeyboardContent{Rows: rows},
	}
}

var (
	_ platform.Sender         = (*qqSender)(nil)
	_ platform.MessageDeleter = (*qqSender)(nil)
	_ platform.ReactionSender = (*qqSender)(nil)
)
