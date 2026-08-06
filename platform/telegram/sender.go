package telegram

import (
	stdctx "context"
	"fmt"
	"strconv"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

const (
	// maxCallbackAnswerRunes 是 answerCallbackQuery.text 的长度上限（Bot API 规定）。
	maxCallbackAnswerRunes = 200

	// parseModeMarkdown 是发送富文本时使用的 parse_mode。
	//
	// 刻意不用 MarkdownV2：MarkdownV2 要求把 _ * [ ] ( ) ~ ` > # + - = | { } . !
	// 全部反斜杠转义（包括普通散文里的句号和连字符），而 OutboundMessage.Markdown
	// 是平台中立字段，内容由插件按通用 Markdown 书写，同一段文本在 Discord 侧
	// 直接可用。用 MarkdownV2 会让几乎每一条带句号的消息都以
	// "can't parse entities" 400 失败且完全不投递。
	//
	// 传统 Markdown 不保留 . - ! 等字符，覆盖了绝大多数常见写法；
	// 剩余的边界情况（如 snake_case 里不成对的下划线）由发送侧的
	// 纯文本降级重发兜底，见 Send/Edit 中的 IsParseEntitiesError 分支。
	parseModeMarkdown = "Markdown"
)

// telegramSender implements platform.Sender and optional extension interfaces
// for the Telegram Bot API.
//
// Supported optional interfaces:
//   - platform.MessageEditor  (Edit)
//   - platform.MessageDeleter (Delete)
//   - platform.ReactionSender (AddReaction, RemoveReaction)
//   - platform.TypingNotifier (SendTyping)
type telegramSender struct {
	client *Client
	botID  string
}

// newSender creates a telegramSender wrapping the given Client.
func newSender(client *Client, botID string) *telegramSender {
	return &telegramSender{client: client, botID: botID}
}

// PlatformAPI 实现 platform.APIProvider，返回 Telegram 封装客户端，
// 调用方可断言 *telegram.Client 访问 Telegram Bot API 的全部能力。
func (s *telegramSender) PlatformAPI() any { return s.client }

// 编译期接口实现检查。
var _ platform.APIProvider = (*telegramSender)(nil)

// Send implements platform.Sender.
//
// Routing logic:
//   - If Target.Tokens has "callback_id", it answers the callback query and sends
//     a follow-up message if there is content.
//   - Otherwise, sends to chatID via the appropriate API method based on content:
//     Markdown → sendMessage with ParseMode=Markdown（解析失败自动降级为纯文本）
//     Text     → sendMessage with plain text
//     Photo    → sendPhoto (URL or binary upload)
//     Audio    → sendAudio
//     Video    → sendVideo
//     File     → sendDocument
func (s *telegramSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if err := req.Validate(); err != nil {
		return platform.SendResult{}, err
	}

	chatID := req.Target.ID
	msg := req.Message
	extra := extractExtra(msg)
	replyToID := parseMessageID(msg.ReplyToID)

	if callbackID := req.Target.Tokens[TokenCallbackID]; callbackID != "" {
		return s.sendCallbackResponse(ctx, callbackID, chatID, msg)
	}

	markup := buildInlineKeyboard(msg.Buttons)

	text := msg.Markdown
	parseMode := parseModeMarkdown
	if text == "" {
		text = msg.Text
		parseMode = ""
	}

	if len(msg.Attachments) > 0 {
		return s.sendWithAttachment(ctx, chatID, text, parseMode, replyToID, msg.Attachments, markup, extra)
	}

	if text == "" && len(msg.Buttons) == 0 {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrUnsupported, PlatformID, chatID,
			"no content to send", 0, nil,
		)
	}

	payload := &SendMessagePayload{
		ChatID:            chatID,
		Text:              text,
		ParseMode:         parseMode,
		ReplyToMessageID:  replyToID,
		ReplyMarkup:       markup,
		DisableWebPreview: extra.DisableWebPreview,
		MessageOptions:    extra.messageOptions(),
	}
	resp, err := s.client.SendMessage(ctx, payload)
	if err != nil && parseMode != "" && IsParseEntitiesError(err) {
		// 富文本解析失败时降级为纯文本重发。
		//
		// msg.Markdown 是框架的平台中立字段（Discord 直接当自己的 Content 用），
		// 内容风格由插件决定，无法保证符合 Telegram 某一种 parse_mode 的语法。
		// 与其让一处格式字符吃掉整条消息，不如去掉 parse_mode 重发一次：
		// 用户看到的是带原始标记的纯文本，而不是什么都收不到。
		logger.WithError(err).
			Warn("[telegram.Sender] 富文本解析失败，降级为纯文本重发")
		payload.ParseMode = ""
		resp, err = s.client.SendMessage(ctx, payload)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}

	msgID := extractMessageID(resp)
	return platform.SendResult{
		Platform:  PlatformID,
		MessageID: msgID,
	}, nil
}

// sendCallbackResponse answers a callback query and optionally sends a follow-up.
func (s *telegramSender) sendCallbackResponse(ctx stdctx.Context, callbackID, chatID string, msg platform.OutboundMessage) (platform.SendResult, error) {
	// answerCallbackQuery.text 上限为 200 字符，超长会被 API 拒绝。
	// 必须截断：否则一条长回复会让 answer 失败，用户既收不到 toast，
	// 也收不到下面的后续消息，按钮还会一直转圈到 Telegram 超时。
	answerText := platform.TruncateText(msg.Text, maxCallbackAnswerRunes)

	// ack 失败不再直接中断流程：真正的内容由下面的 Send 投递，
	// 不应被 ack 的失败连坐。但若本次没有后续消息，ack 本身就是全部投递，
	// 此时必须把错误如实返回，否则重试/指标/错误中间件将什么都看不到。
	ackErr := s.client.AnswerCallbackQuery(ctx, &AnswerCallbackQueryPayload{
		CallbackQueryID: callbackID,
		Text:            answerText,
		ShowAlert:       false,
	})
	if ackErr != nil {
		logger.WithError(ackErr).
			Warn("[telegram.Sender] answerCallbackQuery 失败")
	}

	if !msg.IsEmpty() || len(msg.Buttons) > 0 {
		msg.ReplyToID = ""
		req := platform.SendRequest{Target: platform.ChatInfo{ID: chatID}, Message: msg}
		req.Target.Tokens = nil
		return s.Send(ctx, req)
	}

	// 纯 ack 场景：没有后续消息，ack 就是全部投递，其失败必须上报。
	if ackErr != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			ackErr.Error(), 0, ackErr,
		)
	}

	return platform.SendResult{Platform: PlatformID}, nil
}

// sendWithAttachment dispatches to the appropriate send method based on attachment type.
//
// Only the first attachment is sent (Telegram does not support multiple inline
// attachments in a single message; use sendMediaGroup for Telegram-native multi-attach).
func (s *telegramSender) sendWithAttachment(
	ctx stdctx.Context, chatID, caption, parseMode string,
	replyToID int, atts []platform.Attachment,
	markup *InlineKeyboardMarkup, extra MessageExtra,
) (platform.SendResult, error) {
	att := atts[0]

	send := func(pm string) (platform.SendResult, error) {
		if len(att.Data) > 0 {
			return s.sendBinaryAttachment(ctx, chatID, caption, pm, replyToID, att, markup, extra)
		}
		return s.sendURLAttachment(ctx, chatID, caption, pm, replyToID, att, markup, extra)
	}

	res, err := send(parseMode)
	if err != nil && parseMode != "" && IsParseEntitiesError(err) {
		// 与 Send/Edit 一致：caption 富文本解析失败时去掉 parse_mode 重发。
		// 否则 caption 里一个不成对的下划线就会让整条带附件的消息发不出去。
		// （SendError 实现了 Unwrap，IsParseEntitiesError 能穿透到 *APIError。）
		logger.WithError(err).
			Warn("[telegram.Sender] caption 富文本解析失败，降级为纯文本重发")
		res, err = send("")
	}
	return res, err
}

// sendBinaryAttachment uploads binary data as a file via multipart/form-data.
func (s *telegramSender) sendBinaryAttachment(
	ctx stdctx.Context, chatID, caption, parseMode string,
	replyToID int, att platform.Attachment,
	markup *InlineKeyboardMarkup, extra MessageExtra,
) (platform.SendResult, error) {
	fileName := att.Name
	if fileName == "" {
		fileName = "file"
	}

	var resp []byte
	var err error

	switch att.Kind {
	case platform.AttachmentKindImage:
		sp := &SendPhotoPayload{
			ChatID:           chatID,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		}
		ext := extensionFromMIME(att.MimeType, ".jpg")
		resp, err = s.client.SendPhotoUpload(ctx, sp, fileName+ext, att.Data)

	case platform.AttachmentKindAudio:
		sa := &SendAudioPayload{
			ChatID:           chatID,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		}
		ext := extensionFromMIME(att.MimeType, ".mp3")
		resp, err = s.client.SendAudioUpload(ctx, sa, fileName+ext, att.Data)

	case platform.AttachmentKindVideo:
		sv := &SendVideoPayload{
			ChatID:           chatID,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		}
		ext := extensionFromMIME(att.MimeType, ".mp4")
		resp, err = s.client.SendVideoUpload(ctx, sv, fileName+ext, att.Data)

	default:
		sd := &SendDocumentPayload{
			ChatID:           chatID,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		}
		resp, err = s.client.SendDocumentUpload(ctx, sd, fileName, att.Data)
	}

	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}

	msgID := extractMessageID(resp)
	return platform.SendResult{
		Platform:  PlatformID,
		MessageID: msgID,
	}, nil
}

// sendURLAttachment sends a file by URL or Telegram file_id.
func (s *telegramSender) sendURLAttachment(
	ctx stdctx.Context, chatID, caption, parseMode string,
	replyToID int, att platform.Attachment,
	markup *InlineKeyboardMarkup, extra MessageExtra,
) (platform.SendResult, error) {
	fileIDorURL := att.URL

	var resp []byte
	var err error

	switch att.Kind {
	case platform.AttachmentKindImage:
		resp, err = s.client.SendPhoto(ctx, &SendPhotoPayload{
			ChatID:           chatID,
			Photo:            fileIDorURL,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		})
	case platform.AttachmentKindAudio:
		resp, err = s.client.SendAudio(ctx, &SendAudioPayload{
			ChatID:           chatID,
			Audio:            fileIDorURL,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		})
	case platform.AttachmentKindVideo:
		resp, err = s.client.SendVideo(ctx, &SendVideoPayload{
			ChatID:           chatID,
			Video:            fileIDorURL,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		})
	default:
		resp, err = s.client.SendDocument(ctx, &SendDocumentPayload{
			ChatID:           chatID,
			Document:         fileIDorURL,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
			MessageOptions:   extra.messageOptions(),
		})
	}

	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}

	msgID := extractMessageID(resp)
	return platform.SendResult{
		Platform:  PlatformID,
		MessageID: msgID,
	}, nil
}

// ── platform.MessageEditor ──────────────────────────────────────────────────

// Edit implements platform.MessageEditor.
//
// Edits a previously sent message. Supports text, Markdown, and inline keyboard
// updates. Does not support changing attachment media.
func (s *telegramSender) Edit(ctx stdctx.Context, chatID, messageID string, msg platform.OutboundMessage) error {
	mid, err := strconv.Atoi(messageID)
	if err != nil {
		return fmt.Errorf("telegram sender: invalid messageID %q: %w", messageID, err)
	}
	text := msg.Markdown
	parseMode := parseModeMarkdown
	if text == "" {
		text = msg.Text
		parseMode = ""
	}

	markup := buildInlineKeyboard(msg.Buttons)
	extra := extractExtra(msg)

	payload := &EditMessageTextPayload{
		ChatID:            chatID,
		MessageID:         mid,
		Text:              text,
		ParseMode:         parseMode,
		ReplyMarkup:       markup,
		DisableWebPreview: extra.DisableWebPreview,
	}
	resp, apiErr := s.client.EditMessageText(ctx, payload)
	if apiErr != nil && parseMode != "" && IsParseEntitiesError(apiErr) {
		// 与 Send 一致：解析失败降级为纯文本重试，而不是丢掉这次编辑。
		logger.WithError(apiErr).
			Warn("[telegram.Sender] 编辑时富文本解析失败，降级为纯文本重试")
		payload.ParseMode = ""
		resp, apiErr = s.client.EditMessageText(ctx, payload)
	}
	if apiErr != nil {
		return platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			apiErr.Error(), 0, apiErr,
		)
	}
	_ = resp
	return nil
}

// ── platform.MessageDeleter ─────────────────────────────────────────────────

// Delete implements platform.MessageDeleter.
//
// Deletes a message using the Telegram Bot API. The bot must have appropriate
// permissions in the chat. Messages can only be deleted within 48 hours of
// sending (Telegram limitation).
func (s *telegramSender) Delete(ctx stdctx.Context, chatID, messageID string) error {
	mid, err := strconv.Atoi(messageID)
	if err != nil {
		return fmt.Errorf("telegram sender: invalid messageID %q: %w", messageID, err)
	}
	if err := s.client.DeleteMessage(ctx, &DeleteMessagePayload{
		ChatID:    chatID,
		MessageID: mid,
	}); err != nil {
		return platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}
	return nil
}

// ── platform.ReactionSender ─────────────────────────────────────────────────

// AddReaction implements platform.ReactionSender.
//
// Uses the setMessageReaction API to add a reaction emoji to a message.
// Unicode emoji uses Value directly; custom emoji uses ID.
func (s *telegramSender) AddReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	mid, err := strconv.Atoi(messageID)
	if err != nil {
		return fmt.Errorf("telegram sender: invalid messageID %q: %w", messageID, err)
	}
	emojiStr := emoji.Value
	if emoji.Kind == platform.EmojiKindCustom && emoji.ID != "" {
		emojiStr = emoji.ID
	}
	if err := s.client.SetMessageReaction(ctx, &SetMessageReactionPayload{
		ChatID:    chatID,
		MessageID: mid,
		Emoji:     emojiStr,
		IsBig:     false,
	}); err != nil {
		return platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}
	return nil
}

// RemoveReaction implements platform.ReactionSender.
//
// Calls setMessageReaction to remove a specific reaction emoji from a message.
// If the emoji is empty, all reactions from the bot are removed.
func (s *telegramSender) RemoveReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	mid, err := strconv.Atoi(messageID)
	if err != nil {
		return fmt.Errorf("telegram sender: invalid messageID %q: %w", messageID, err)
	}
	emojiStr := emoji.Value
	if emoji.Kind == platform.EmojiKindCustom && emoji.ID != "" {
		emojiStr = emoji.ID
	}
	if err := s.client.SetMessageReaction(ctx, &SetMessageReactionPayload{
		ChatID:    chatID,
		MessageID: mid,
		Emoji:     emojiStr,
		IsBig:     false,
	}); err != nil {
		return platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}
	return nil
}

// ── platform.TypingNotifier ─────────────────────────────────────────────────

// SendTyping implements platform.TypingNotifier.
//
// Sends the "typing" chat action indicator. Telegram automatically stops the
// indicator after a few seconds. Call periodically for long operations.
func (s *telegramSender) SendTyping(ctx stdctx.Context, chatID string) error {
	if err := s.client.SendChatAction(ctx, &SendChatActionPayload{
		ChatID: chatID,
		Action: "typing",
	}); err != nil {
		return platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

// buildInlineKeyboard converts platform.Buttons to Telegram InlineKeyboardMarkup.
//
// Buttons with Row == ButtonRowAuto (0) each get their own row.
// Buttons with the same Row value (1–5) are grouped into the same row.
// Link-style buttons use URL; all others use CallbackData.
func buildInlineKeyboard(buttons []platform.Button) *InlineKeyboardMarkup {
	if len(buttons) == 0 {
		return nil
	}

	type rowEntry struct {
		row      int
		children []InlineKeyboardButton
	}

	var rows []rowEntry
	rowIdx := make(map[int]int)

	for _, btn := range buttons {
		tb := InlineKeyboardButton{
			Text: btn.Label,
		}

		if btn.Style == platform.ButtonStyleLink || btn.URL != "" {
			tb.URL = btn.URL
		} else {
			tb.CallbackData = btn.ID
		}

		if btn.Extra != nil {
			if ext, ok := btn.Extra[ExtraKeyInline].(*InlineButtonExtra); ok {
				tb.SwitchInline = ext.SwitchInlineQuery
			}
		}

		row := btn.Row
		if row <= 0 {
			rows = append(rows, rowEntry{row: -(len(rows) + 1), children: []InlineKeyboardButton{tb}})
			continue
		}

		if idx, ok := rowIdx[row]; ok {
			rows[idx].children = append(rows[idx].children, tb)
		} else {
			rowIdx[row] = len(rows)
			rows = append(rows, rowEntry{row: row, children: []InlineKeyboardButton{tb}})
		}
	}

	keyboard := make([][]InlineKeyboardButton, len(rows))
	for i, r := range rows {
		keyboard[i] = r.children
	}
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

// parseMessageID parses a string message ID to int (Telegram's native format).
//
// Returns 0 if the string is empty or not a valid integer. Telegram message
// IDs are positive integers, so 0 is a safe sentinel for "not set".
func parseMessageID(id string) int {
	if id == "" {
		return 0
	}
	n, _ := strconv.Atoi(id)
	return n
}

// extractMessageID extracts the message ID from a raw Telegram API response.
//
// The response JSON is searched for "message_id":<number> using a simple string
// scan. This avoids a full JSON unmarshal just to get the message ID.
func extractMessageID(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	idx := strings.Index(string(data), `"message_id"`)
	if idx < 0 {
		return ""
	}
	rest := string(data)[idx+12:]
	start := -1
	for i, c := range rest {
		if c >= '0' && c <= '9' {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			return rest[start:i]
		}
	}
	if start >= 0 {
		return rest[start:]
	}
	return ""
}

// extensionFromMIME returns a file extension for a given MIME type.
//
// Falls back to the provided fallback value for unknown MIME types.
func extensionFromMIME(mime, fallback string) string {
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/wav":
		return ".wav"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	default:
		return fallback
	}
}

// compile-time interface checks
var (
	_ platform.Sender         = (*telegramSender)(nil)
	_ platform.MessageEditor  = (*telegramSender)(nil)
	_ platform.MessageDeleter = (*telegramSender)(nil)
	_ platform.ReactionSender = (*telegramSender)(nil)
	_ platform.TypingNotifier = (*telegramSender)(nil)
)
