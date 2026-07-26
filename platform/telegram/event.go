package telegram

import (
	"strconv"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// telegramEvent wraps a Telegram Update as a platform.Event.
//
// Implements the core platform.Event interface plus the optional extension
// interfaces: RawEvent, ReplyEvent, EditableEvent, MentionsEvent.
type telegramEvent struct {
	kind        platform.EventKind
	rawType     string
	rawPayload  any
	id          string
	senderInfo  platform.UserInfo
	chat        platform.ChatInfo
	content     string
	timestamp   time.Time
	attachments []platform.InboundAttachment
	replyToID   string
	isEdited    bool
	origTS      time.Time
	mentions    []platform.UserInfo
}

// ── platform.Event ──────────────────────────────────────────────────────────

func (e *telegramEvent) Platform() string                          { return PlatformID }
func (e *telegramEvent) Kind() platform.EventKind                  { return e.kind }
func (e *telegramEvent) ID() string                                { return e.id }
func (e *telegramEvent) Sender() platform.UserInfo                 { return e.senderInfo }
func (e *telegramEvent) Chat() platform.ChatInfo                   { return e.chat }
func (e *telegramEvent) Content() string                           { return e.content }
func (e *telegramEvent) Timestamp() time.Time                      { return e.timestamp }
func (e *telegramEvent) Attachments() []platform.InboundAttachment { return e.attachments }

// ── platform.RawEvent ───────────────────────────────────────────────────────

func (e *telegramEvent) RawType() string { return e.rawType }
func (e *telegramEvent) RawPayload() any { return e.rawPayload }

// ── platform.ReplyEvent ─────────────────────────────────────────────────────

func (e *telegramEvent) ReplyToID() string { return e.replyToID }

// ── platform.EditableEvent ──────────────────────────────────────────────────

func (e *telegramEvent) IsEdited() bool               { return e.isEdited }
func (e *telegramEvent) OriginalTimestamp() time.Time { return e.origTS }

// ── platform.MentionsEvent ──────────────────────────────────────────────────

func (e *telegramEvent) Mentions() []platform.UserInfo { return e.mentions }

// compile-time interface checks
var (
	_ platform.Event         = (*telegramEvent)(nil)
	_ platform.RawEvent      = (*telegramEvent)(nil)
	_ platform.ReplyEvent    = (*telegramEvent)(nil)
	_ platform.EditableEvent = (*telegramEvent)(nil)
	_ platform.MentionsEvent = (*telegramEvent)(nil)
)

// newEvent converts a Telegram Update to a platform.Event.
//
// Returns nil if the update type is not supported or recognized.
func newEvent(upd *Update) platform.Event {
	if upd == nil {
		return nil
	}

	switch {
	case upd.Message != nil:
		return newMessageEvent(upd.Message, false)
	case upd.EditedMessage != nil:
		return newMessageEvent(upd.EditedMessage, true)
	case upd.ChannelPost != nil:
		return newMessageEvent(upd.ChannelPost, false)
	case upd.CallbackQuery != nil:
		return newCallbackQueryEvent(upd.CallbackQuery)
	case upd.MyChatMember != nil:
		return newChatMemberEvent(upd.MyChatMember)
	default:
		return nil
	}
}

// newMessageEvent creates a platform.Event from a Telegram Message.
//
// When edited is true the event is marked as a message edit (MESSAGE_UPDATE)
// and the EditableEvent interface returns isEdited = true.
func newMessageEvent(msg *Message, edited bool) platform.Event {
	e := &telegramEvent{
		rawType:    "message",
		rawPayload: msg,
		id:         strconv.Itoa(msg.MessageID),
		timestamp:  time.Unix(msg.Date, 0),
		isEdited:   edited,
	}

	if edited {
		e.rawType = "edited_message"
		e.origTS = e.timestamp
	}

	if msg.From != nil {
		e.senderInfo = userFromTelegram(msg.From)
	}
	e.chat = chatFromTelegram(msg.Chat)

	switch msg.Chat.Type {
	case ChatTypePrivate:
		e.kind = platform.EventKindPrivateMessage
	case ChatTypeGroup, ChatTypeSupergroup:
		e.kind = platform.EventKindGroupMessage
	case ChatTypeChannel:
		e.kind = platform.EventKindGuildMessage
	}

	e.content = msg.Text
	if e.content == "" {
		e.content = msg.Caption
	}

	if msg.ReplyToMsg != nil {
		e.replyToID = strconv.Itoa(msg.ReplyToMsg.MessageID)
	}

	e.attachments = collectAttachments(msg)

	if len(msg.Entities) > 0 {
		mentions := make([]platform.UserInfo, 0)
		for _, ent := range msg.Entities {
			if ent.Type == "mention" && ent.User != nil {
				mentions = append(mentions, userFromTelegram(ent.User))
			}
		}
		e.mentions = mentions
	}

	return e
}

// newCallbackQueryEvent creates a platform.Event from a Telegram CallbackQuery.
//
// The callback query ID is stored in ChatInfo.Tokens[TokenCallbackID] so the
// sender can call answerCallbackQuery when replying to this interaction.
func newCallbackQueryEvent(cq *CallbackQuery) platform.Event {
	e := &telegramEvent{
		kind:       platform.EventKindInteraction,
		rawType:    "callback_query",
		rawPayload: cq,
		id:         cq.ID,
		content:    cq.Data,
	}

	if cq.From != nil {
		e.senderInfo = userFromTelegram(cq.From)
	}

	if cq.Message != nil && cq.Message.Chat != nil {
		e.chat = chatFromTelegram(cq.Message.Chat)
		e.timestamp = time.Unix(cq.Message.Date, 0)
		if e.chat.Tokens == nil {
			e.chat.Tokens = make(map[string]string)
		}
		e.chat.Tokens[TokenCallbackID] = cq.ID
	}

	return e
}

// newChatMemberEvent creates a platform.Event from a ChatMemberUpdated update.
//
// Maps "member"/"administrator" status → BOT_ADDED, "left"/"kicked" → BOT_REMOVED.
// Other status values return nil (ignored).
func newChatMemberEvent(upd *ChatMemberUpdated) platform.Event {
	e := &telegramEvent{
		rawPayload: upd,
		chat:       chatFromTelegram(upd.Chat),
		timestamp:  time.Unix(upd.Date, 0),
	}

	if upd.From != nil {
		e.senderInfo = userFromTelegram(upd.From)
	}

	status := ""
	if upd.NewChatMember != nil {
		status = upd.NewChatMember.Status
	}

	switch status {
	case "member", "administrator":
		e.kind = platform.EventKindBotAdded
		e.rawType = "bot_added"
	case "left", "kicked":
		e.kind = platform.EventKindBotRemoved
		e.rawType = "bot_removed"
	default:
		return nil
	}

	return e
}

// userFromTelegram converts a Telegram User to platform.UserInfo.
func userFromTelegram(u *User) platform.UserInfo {
	if u == nil {
		return platform.UserInfo{}
	}
	return platform.UserInfo{
		ID:          strconv.FormatInt(u.ID, 10),
		DisplayName: u.DisplayName(),
		IsBot:       u.IsBot,
	}
}

// chatFromTelegram converts a Telegram Chat to platform.ChatInfo.
func chatFromTelegram(c *Chat) platform.ChatInfo {
	ci := platform.ChatInfo{
		ID:   strconv.FormatInt(c.ID, 10),
		Name: c.DisplayName(),
	}

	switch c.Type {
	case ChatTypeGroup, ChatTypeSupergroup:
		ci.IsGroup = true
	case ChatTypePrivate:
		ci.IsGroup = false
	case ChatTypeChannel:
		ci.IsGroup = true
	}

	return ci
}

// collectAttachments extracts media attachments from a Telegram Message.
//
// Handles Photo, Audio, Video, Document, Voice, Animation, and Sticker.
// For photos, the largest size (last in the array) is used.
func collectAttachments(msg *Message) []platform.InboundAttachment {
	var atts []platform.InboundAttachment

	if len(msg.Photo) > 0 {
		p := msg.Photo[len(msg.Photo)-1]
		atts = append(atts, platform.InboundAttachment{
			URL:    p.FileID,
			Width:  p.Width,
			Height: p.Height,
			Size:   p.FileSize,
		})
	}
	if msg.Audio != nil {
		atts = append(atts, platform.InboundAttachment{
			URL:      msg.Audio.FileID,
			MimeType: msg.Audio.MimeType,
			Name:     msg.Audio.FileName,
			Size:     msg.Audio.FileSize,
		})
	}
	if msg.Video != nil {
		atts = append(atts, platform.InboundAttachment{
			URL:      msg.Video.FileID,
			MimeType: msg.Video.MimeType,
			Name:     msg.Video.FileName,
			Width:    msg.Video.Width,
			Height:   msg.Video.Height,
			Size:     msg.Video.FileSize,
		})
	}
	if msg.Document != nil {
		atts = append(atts, platform.InboundAttachment{
			URL:      msg.Document.FileID,
			MimeType: msg.Document.MimeType,
			Name:     msg.Document.FileName,
			Size:     msg.Document.FileSize,
		})
	}
	if msg.Voice != nil {
		atts = append(atts, platform.InboundAttachment{
			URL:      msg.Voice.FileID,
			MimeType: msg.Voice.MimeType,
			Size:     msg.Voice.FileSize,
		})
	}
	if msg.Animation != nil {
		atts = append(atts, platform.InboundAttachment{
			URL:    msg.Animation.FileID,
			Width:  msg.Animation.Width,
			Height: msg.Animation.Height,
			Size:   msg.Animation.FileSize,
		})
	}
	if msg.Sticker != nil {
		atts = append(atts, platform.InboundAttachment{
			URL:    msg.Sticker.FileID,
			Width:  msg.Sticker.Width,
			Height: msg.Sticker.Height,
			Size:   msg.Sticker.FileSize,
		})
	}

	return atts
}
