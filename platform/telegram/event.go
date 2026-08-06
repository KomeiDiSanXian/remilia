package telegram

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

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
	attachments []platform.Attachment
	replyToID   string
	isEdited    bool
	origTS      time.Time
	mentions    []platform.UserInfo
}

// ── platform.Event ──────────────────────────────────────────────────────────

func (e *telegramEvent) Platform() string                   { return PlatformID }
func (e *telegramEvent) Kind() platform.EventKind           { return e.kind }
func (e *telegramEvent) ID() string                         { return e.id }
func (e *telegramEvent) Sender() platform.UserInfo          { return e.senderInfo }
func (e *telegramEvent) Chat() platform.ChatInfo            { return e.chat }
func (e *telegramEvent) Content() string                    { return e.content }
func (e *telegramEvent) Timestamp() time.Time               { return e.timestamp }
func (e *telegramEvent) Attachments() []platform.Attachment { return e.attachments }

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

	// 编辑消息映射为 MESSAGE_UPDATE，与包文档（doc.go 的事件映射表）
	// 以及 Discord 适配器保持一致。
	//
	// 此前 edited 只设置 rawType/isEdited，Kind() 仍然返回普通消息类型，
	// 于是一条编辑会以新消息的身份重新进入命令管道：用户先发 "hello"，
	// 20 分钟后把它改成 "/ban @victim"，命令照样执行，而审计日志里
	// 留下的还是那句人畜无害的原文。
	if edited {
		e.kind = platform.EventKindMessageUpdate
	}

	e.content = msg.Text
	if e.content == "" {
		e.content = msg.Caption
	}

	if msg.ReplyToMsg != nil {
		e.replyToID = strconv.Itoa(msg.ReplyToMsg.MessageID)
	}

	e.attachments = collectAttachments(msg)

	e.mentions = collectMentions(msg)

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

// collectMentions 提取消息中被 @ 的用户。
//
// Telegram 用两种实体表示 @：
//
//   - "text_mention"：针对无用户名的用户，实体自带完整 User 对象；
//   - "mention"：普通 "@username" 形式，实体**不含** User（Bot API 明确
//     规定 user 字段仅用于 text_mention），只能从正文按偏移切出用户名。
//
// 此前的实现只匹配 `Type == "mention" && User != nil`，这个条件对真实
// Telegram 报文恒为假，导致 Mentions() 永远为空、OnMentionedBot() 之类的
// 规则在 Telegram 上永不命中。
//
// 注意：Offset/Length 的单位是 UTF-16 代码单元，不是字节也不是 rune，
// 因此必须先把正文编码成 UTF-16 再切片，否则含 emoji 或中文的消息会错位。
func collectMentions(msg *Message) []platform.UserInfo {
	if len(msg.Entities) == 0 {
		return nil
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	var u16 []uint16 // 惰性编码：无 "mention" 实体时无需付出编码开销

	mentions := make([]platform.UserInfo, 0, len(msg.Entities))
	for _, ent := range msg.Entities {
		switch ent.Type {
		case "text_mention":
			if ent.User != nil {
				mentions = append(mentions, userFromTelegram(ent.User))
			}
		case "mention":
			if u16 == nil {
				u16 = utf16.Encode([]rune(text))
			}
			if ent.Offset < 0 || ent.Length <= 0 || ent.Offset+ent.Length > len(u16) {
				continue // 越界实体：报文异常，跳过而非 panic
			}
			name := string(utf16.Decode(u16[ent.Offset : ent.Offset+ent.Length]))
			name = strings.TrimPrefix(name, "@")
			if name == "" {
				continue
			}
			// "@username" 形式拿不到数字 ID，只能给出用户名。
			mentions = append(mentions, platform.UserInfo{DisplayName: name})
		}
	}
	if len(mentions) == 0 {
		return nil
	}
	return mentions
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

// FileMeta 是 Telegram 附件的平台专属元数据，挂在
// platform.Attachment.Extra 上。
//
// Telegram 的入站附件只携带不透明的 file_id，必须先调用 getFile 换取
// file_path，再拼出下载地址；且下载地址的路径里嵌着 bot token。
// 因此 file_id 单独放在这里，由适配器负责解析出真正可用的 URL
// （见 PollingAdapter.resolveAttachmentURLs），插件也可以拿着它
// 调用 Client.DownloadFile 自行下载。
type FileMeta struct {
	// FileID 是 Telegram 的文件标识符，可用于 getFile / 二次发送。
	FileID string
	// FileUniqueID 是跨机器人稳定的唯一标识（部分附件类型才有）。
	FileUniqueID string
}

// collectAttachments extracts media attachments from a Telegram Message.
//
// Handles Photo, Audio, Video, Document, Voice, Animation, and Sticker.
// For photos, the largest size (last in the array) is used.
//
// 注意：这里**不**填充 URL。Telegram 不在 update 里给出可下载地址，
// 换取地址需要一次额外的 getFile 调用。此前把 file_id 直接塞进 URL 字段，
// 跨平台插件对 att.URL 执行 http.Get 会得到
// "unsupported protocol scheme \"\"" ——既不能用，也不报错在点子上。
// file_id 现在放进 Extra，URL 由适配器解析后回填。
func collectAttachments(msg *Message) []platform.Attachment {
	var atts []platform.Attachment

	if len(msg.Photo) > 0 {
		p := msg.Photo[len(msg.Photo)-1]
		atts = append(atts, platform.Attachment{
			Width:  p.Width,
			Height: p.Height,
			Size:   p.FileSize,
			Extra:  map[string]any{ExtraKeyFile: &FileMeta{FileID: p.FileID, FileUniqueID: p.FileUniqueID}},
		})
	}
	if msg.Audio != nil {
		atts = append(atts, platform.Attachment{
			MimeType: msg.Audio.MimeType,
			Name:     msg.Audio.FileName,
			Size:     msg.Audio.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Audio.FileID, FileUniqueID: msg.Audio.FileUniqueID}},
		})
	}
	if msg.Video != nil {
		atts = append(atts, platform.Attachment{
			MimeType: msg.Video.MimeType,
			Name:     msg.Video.FileName,
			Width:    msg.Video.Width,
			Height:   msg.Video.Height,
			Size:     msg.Video.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Video.FileID, FileUniqueID: msg.Video.FileUniqueID}},
		})
	}
	if msg.Document != nil {
		atts = append(atts, platform.Attachment{
			MimeType: msg.Document.MimeType,
			Name:     msg.Document.FileName,
			Size:     msg.Document.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Document.FileID, FileUniqueID: msg.Document.FileUniqueID}},
		})
	}
	if msg.Voice != nil {
		atts = append(atts, platform.Attachment{
			MimeType: msg.Voice.MimeType,
			Size:     msg.Voice.FileSize,
			Extra:    map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Voice.FileID, FileUniqueID: msg.Voice.FileUniqueID}},
		})
	}
	if msg.Animation != nil {
		atts = append(atts, platform.Attachment{
			Width:  msg.Animation.Width,
			Height: msg.Animation.Height,
			Size:   msg.Animation.FileSize,
			Extra:  map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Animation.FileID, FileUniqueID: msg.Animation.FileUniqueID}},
		})
	}
	if msg.Sticker != nil {
		atts = append(atts, platform.Attachment{
			Width:  msg.Sticker.Width,
			Height: msg.Sticker.Height,
			Size:   msg.Sticker.FileSize,
			Extra:  map[string]any{ExtraKeyFile: &FileMeta{FileID: msg.Sticker.FileID, FileUniqueID: msg.Sticker.FileUniqueID}},
		})
	}

	return atts
}
