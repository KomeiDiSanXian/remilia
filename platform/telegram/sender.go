package telegram

import (
	stdctx "context"
	"fmt"
	"strconv"
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
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

// Send implements platform.Sender.
//
// Routing logic:
//   - If Target.Tokens has "callback_id", it answers the callback query and sends
//     a follow-up message if there is content.
//   - Otherwise, sends to chatID via the appropriate API method based on content:
//     Markdown → sendMessage with ParseMode=MarkdownV2
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
	parseMode := "MarkdownV2"
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

	resp, err := s.client.SendMessage(ctx, &SendMessagePayload{
		ChatID:           chatID,
		Text:             text,
		ParseMode:        parseMode,
		ReplyToMessageID: replyToID,
		ReplyMarkup:      markup,
	})
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
	answerText := ""
	if msg.Text != "" {
		answerText = msg.Text
	}
	if err := s.client.AnswerCallbackQuery(ctx, &AnswerCallbackQueryPayload{
		CallbackQueryID: callbackID,
		Text:            answerText,
		ShowAlert:       false,
	}); err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, PlatformID, chatID,
			err.Error(), 0, err,
		)
	}

	if !msg.IsEmpty() || len(msg.Buttons) > 0 {
		msg.ReplyToID = ""
		req := platform.SendRequest{Target: platform.ChatInfo{ID: chatID}, Message: msg}
		req.Target.Tokens = nil
		return s.Send(ctx, req)
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

	if len(att.Data) > 0 {
		return s.sendBinaryAttachment(ctx, chatID, caption, parseMode, replyToID, att, markup)
	}
	return s.sendURLAttachment(ctx, chatID, caption, parseMode, replyToID, att, markup)
}

// sendBinaryAttachment uploads binary data as a file via multipart/form-data.
func (s *telegramSender) sendBinaryAttachment(
	ctx stdctx.Context, chatID, caption, parseMode string,
	replyToID int, att platform.Attachment,
	markup *InlineKeyboardMarkup,
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
	markup *InlineKeyboardMarkup,
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
		})
	case platform.AttachmentKindAudio:
		resp, err = s.client.SendAudio(ctx, &SendAudioPayload{
			ChatID:           chatID,
			Audio:            fileIDorURL,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
		})
	case platform.AttachmentKindVideo:
		resp, err = s.client.SendVideo(ctx, &SendVideoPayload{
			ChatID:           chatID,
			Video:            fileIDorURL,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
		})
	default:
		resp, err = s.client.SendDocument(ctx, &SendDocumentPayload{
			ChatID:           chatID,
			Document:         fileIDorURL,
			Caption:          caption,
			ParseMode:        parseMode,
			ReplyToMessageID: replyToID,
			ReplyMarkup:      markup,
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
	parseMode := "MarkdownV2"
	if text == "" {
		text = msg.Text
		parseMode = ""
	}

	markup := buildInlineKeyboard(msg.Buttons)

	resp, apiErr := s.client.EditMessageText(ctx, &EditMessageTextPayload{
		ChatID:      chatID,
		MessageID:   mid,
		Text:        text,
		ParseMode:   parseMode,
		ReplyMarkup: markup,
	})
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
			if ext, ok := btn.Extra.(*InlineButtonExtra); ok {
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
