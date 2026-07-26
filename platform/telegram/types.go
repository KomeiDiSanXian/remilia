// Package telegram implements the Telegram Bot API platform adapter for remilia.
//
// The adapter uses long polling (getUpdates) to receive events and wraps them into
// platform.Event for the framework core. No external Telegram SDK is required;
// all communication uses net/http to call the Telegram Bot API directly.
//
// # Quick Start
//
//	adapter, err := telegram.NewAdapter("BOT_TOKEN")
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
//
// # Event Mapping
//
//	Telegram Update          → platform.EventKind
//	Message (private)        → PRIVATE_MESSAGE
//	Message (group/super)    → GROUP_MESSAGE
//	EditedMessage            → MESSAGE_UPDATE
//	CallbackQuery            → INTERACTION
//	MyChatMember (member)    → BOT_ADDED
//	MyChatMember (left/kick) → BOT_REMOVED
//
// # Capabilities
//
// Telegram supports Markdown, inline keyboards (buttons), multi-attachment,
// message edit/delete, file upload, reactions, thread reply, and typing indicator.
package telegram

import "strconv"

// PlatformID is the unique platform identifier for Telegram.
const PlatformID = "telegram"

// Update represents an incoming Telegram update from the Bot API.
//
// This is the root JSON object from getUpdates. At most one of the pointer
// fields will be non-nil per update.
type Update struct {
	// UpdateID is the update's unique identifier (monotonically increasing).
	UpdateID int64 `json:"update_id"`
	// Message is a new incoming message (any chat type).
	Message *Message `json:"message,omitempty"`
	// EditedMessage is an edited version of a previously sent message.
	EditedMessage *Message `json:"edited_message,omitempty"`
	// ChannelPost is a new channel post.
	ChannelPost *Message `json:"channel_post,omitempty"`
	// CallbackQuery is an incoming callback query from an inline button press.
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
	// MyChatMember indicates the bot's chat member status changed.
	MyChatMember *ChatMemberUpdated `json:"my_chat_member,omitempty"`
}

// Message represents a Telegram message.
type Message struct {
	// MessageID is the unique message identifier inside this chat.
	MessageID int `json:"message_id"`
	// From is the sender of the message (may be empty for channel posts).
	From *User `json:"from,omitempty"`
	// Chat is the conversation the message belongs to.
	Chat *Chat `json:"chat"`
	// Date is the Unix time the message was sent.
	Date int64 `json:"date"`
	// Text is the UTF-8 text of the message (empty if media-only).
	Text string `json:"text,omitempty"`
	// Caption is the caption for media (photo, video, document, etc.).
	Caption string `json:"caption,omitempty"`
	// Entities are special entities like mentions, hashtags, bot commands.
	Entities []MessageEntity `json:"entities,omitempty"`
	// ReplyToMsg is the original message this message is replying to.
	ReplyToMsg *Message `json:"reply_to_message,omitempty"`
	// Photo is an array of photo sizes (largest last).
	Photo []PhotoSize `json:"photo,omitempty"`
	// Audio is audio file information.
	Audio *Audio `json:"audio,omitempty"`
	// Video is video file information.
	Video *Video `json:"video,omitempty"`
	// Document is a general file.
	Document *Document `json:"document,omitempty"`
	// Voice is a voice message.
	Voice *Voice `json:"voice,omitempty"`
	// Animation is a GIF or H.264/MPEG-4 AVC video without sound.
	Animation *Animation `json:"animation,omitempty"`
	// Sticker is a sticker.
	Sticker *Sticker `json:"sticker,omitempty"`
	// NewChatMembers lists users that were added to the group or supergroup.
	NewChatMembers []User `json:"new_chat_members,omitempty"`
	// LeftChatMember is a user that left the group or supergroup.
	LeftChatMember *User `json:"left_chat_member,omitempty"`
}

// User represents a Telegram user or bot.
type User struct {
	// ID is the user's unique identifier.
	ID int64 `json:"id"`
	// IsBot indicates if this user is a bot.
	IsBot bool `json:"is_bot"`
	// FirstName is the user's first name.
	FirstName string `json:"first_name"`
	// LastName is the user's last name (optional).
	LastName string `json:"last_name,omitempty"`
	// Username is the user's @username (optional).
	Username string `json:"username,omitempty"`
	// LanguageCode is the IETF language tag of the user's language.
	LanguageCode string `json:"language_code,omitempty"`
}

// UserName returns the bot's @username or falls back to FirstName.
func (u *User) UserName() string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return u.FirstName
}

// DisplayName returns a human-readable display name.
func (u *User) DisplayName() string {
	s := u.FirstName
	if u.LastName != "" {
		s += " " + u.LastName
	}
	if u.Username != "" {
		s += " (@" + u.Username + ")"
	}
	return s
}

// Chat represents a Telegram chat (private, group, supergroup, or channel).
type Chat struct {
	// ID is the chat's unique identifier.
	ID int64 `json:"id"`
	// Type is the chat type (private, group, supergroup, or channel).
	Type ChatType `json:"type"`
	// Title is the chat title (groups/channels only).
	Title string `json:"title,omitempty"`
	// Username is the @username (private chats/channels only).
	Username string `json:"username,omitempty"`
	// FirstName is the first name (private chats only).
	FirstName string `json:"first_name,omitempty"`
	// LastName is the last name (private chats only).
	LastName string `json:"last_name,omitempty"`
}

// DisplayName returns a human-readable chat name.
func (c *Chat) DisplayName() string {
	if c.Title != "" {
		return c.Title
	}
	s := c.FirstName
	if c.LastName != "" {
		s += " " + c.LastName
	}
	return s
}

// ChatType is the type of a Telegram chat.
type ChatType string

const (
	// ChatTypePrivate is a one-on-one private chat.
	ChatTypePrivate ChatType = "private"
	// ChatTypeGroup is a basic group chat.
	ChatTypeGroup ChatType = "group"
	// ChatTypeSupergroup is a supergroup (upgraded group).
	ChatTypeSupergroup ChatType = "supergroup"
	// ChatTypeChannel is a broadcast channel.
	ChatTypeChannel ChatType = "channel"
)

// MessageEntity represents a special text entity in a message.
type MessageEntity struct {
	// Type is the entity type (mention, hashtag, bot_command, url, etc.).
	Type string `json:"type"`
	// Offset is the offset in UTF-16 code units to the start of the entity.
	Offset int `json:"offset"`
	// Length is the length of the entity in UTF-16 code units.
	Length int `json:"length"`
	// URL is an optional URL (type "text_link" only).
	URL string `json:"url,omitempty"`
	// User is an optional mentioned user (type "text_mention" only).
	User *User `json:"user,omitempty"`
}

// PhotoSize represents one size of a photo or file thumbnail.
type PhotoSize struct {
	// FileID is the identifier for this file (can be used for download).
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file (permanent).
	FileUniqueID string `json:"file_unique_id"`
	// Width is the photo width.
	Width int `json:"width"`
	// Height is the photo height.
	Height int `json:"height"`
	// FileSize is the file size in bytes.
	FileSize int `json:"file_size,omitempty"`
}

// Audio represents an audio file (voice note or music).
type Audio struct {
	// FileID is the identifier for this file.
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file.
	FileUniqueID string `json:"file_unique_id"`
	// Duration is the duration in seconds.
	Duration int `json:"duration"`
	// Performer is the performer of the audio.
	Performer string `json:"performer,omitempty"`
	// Title is the title of the audio.
	Title string `json:"title,omitempty"`
	// FileName is the original filename.
	FileName string `json:"file_name,omitempty"`
	// MimeType is the MIME type of the file.
	MimeType string `json:"mime_type,omitempty"`
	// FileSize is the file size in bytes.
	FileSize int `json:"file_size,omitempty"`
}

// Video represents a video file.
type Video struct {
	// FileID is the identifier for this file.
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file.
	FileUniqueID string `json:"file_unique_id"`
	// Width is the video width.
	Width int `json:"width"`
	// Height is the video height.
	Height int `json:"height"`
	// Duration is the duration in seconds.
	Duration int `json:"duration"`
	// FileName is the original filename.
	FileName string `json:"file_name,omitempty"`
	// MimeType is the MIME type of the file.
	MimeType string `json:"mime_type,omitempty"`
	// FileSize is the file size in bytes.
	FileSize int `json:"file_size,omitempty"`
}

// Document represents a general file (no audio/video/voice/photo/STT).
type Document struct {
	// FileID is the identifier for this file.
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file.
	FileUniqueID string `json:"file_unique_id"`
	// FileName is the original filename.
	FileName string `json:"file_name,omitempty"`
	// MimeType is the MIME type of the file.
	MimeType string `json:"mime_type,omitempty"`
	// FileSize is the file size in bytes.
	FileSize int `json:"file_size,omitempty"`
	// Thumbnail is the document thumbnail.
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
}

// Voice represents a voice message.
type Voice struct {
	// FileID is the identifier for this file.
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file.
	FileUniqueID string `json:"file_unique_id"`
	// Duration is the duration in seconds.
	Duration int `json:"duration"`
	// MimeType is the MIME type of the file.
	MimeType string `json:"mime_type,omitempty"`
	// FileSize is the file size in bytes.
	FileSize int `json:"file_size,omitempty"`
}

// Animation represents a GIF or H.264/MPEG-4 AVC video without sound.
type Animation struct {
	// FileID is the identifier for this file.
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file.
	FileUniqueID string `json:"file_unique_id"`
	// Width is the video width.
	Width int `json:"width"`
	// Height is the video height.
	Height int `json:"height"`
	// Duration is the duration in seconds.
	Duration int `json:"duration"`
	// FileName is the original filename.
	FileName string `json:"file_name,omitempty"`
	// MimeType is the MIME type of the file.
	MimeType string `json:"mime_type,omitempty"`
	// FileSize is the file size in bytes.
	FileSize int `json:"file_size,omitempty"`
	// Thumbnail is the animation thumbnail.
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
}

// Sticker represents a Telegram sticker.
type Sticker struct {
	// FileID is the identifier for this file.
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file.
	FileUniqueID string `json:"file_unique_id"`
	// Type is the sticker type (regular, mask, custom_emoji).
	Type string `json:"type"`
	// Width is the sticker width.
	Width int `json:"width"`
	// Height is the sticker height.
	Height int `json:"height"`
	// FileSize is the file size in bytes.
	FileSize int `json:"file_size,omitempty"`
}

// CallbackQuery represents an incoming callback query from an inline button.
type CallbackQuery struct {
	// ID is the unique identifier for this query.
	ID string `json:"id"`
	// From is the sender of the query.
	From *User `json:"from"`
	// Message is the message with the callback button (may be empty).
	Message *Message `json:"message,omitempty"`
	// InlineMessageID is the identifier of the message (inline queries only).
	InlineMessageID string `json:"inline_message_id,omitempty"`
	// ChatInstance is the chat instance (global identifier for the chat).
	ChatInstance string `json:"chat_instance"`
	// Data is the callback data associated with the button.
	Data string `json:"data,omitempty"`
}

// ChatMemberUpdated represents a change in a chat member's status.
type ChatMemberUpdated struct {
	// Chat is the chat the user belongs to.
	Chat *Chat `json:"chat"`
	// From is the user who performed the action.
	From *User `json:"from"`
	// Date is the Unix time of the change.
	Date int64 `json:"date"`
	// OldChatMember is the previous member information.
	OldChatMember *ChatMember `json:"old_chat_member"`
	// NewChatMember is the new member information.
	NewChatMember *ChatMember `json:"new_chat_member"`
}

// ChatMember represents information about one chat member.
type ChatMember struct {
	// User is information about the user.
	User *User `json:"user"`
	// Status is the member's status (creator, administrator, member, left, kicked).
	Status string `json:"status"`
}

// ────────────────────────────────────────────────────────────────────────────
// Request payload types
// ────────────────────────────────────────────────────────────────────────────

// MessageOptions 是各发送接口共有的 Telegram 通用可选开关。
//
// 以匿名字段嵌入各 *Payload，encoding/json 会把这些字段平铺到顶层 JSON 对象，
// 与 Bot API 的参数结构一致。
//
// 此前 MessageExtra 里的这些开关在 payload 结构体中根本没有对应字段，
// telegram.ApplyExtra 完全是空转：调用方设置了 DisableNotification，
// 五千人群依然会在凌晨收到推送，且没有任何报错。
type MessageOptions struct {
	// DisableNotification sends the message silently.
	DisableNotification bool `json:"disable_notification,omitempty"`
	// ProtectContent protects the message from being forwarded or saved.
	ProtectContent bool `json:"protect_content,omitempty"`
	// AllowPaidBroadcast allows sending as a paid broadcast (Bot API 7.10+).
	AllowPaidBroadcast bool `json:"allow_paid_broadcast,omitempty"`
}

// SendMessagePayload is the JSON body for the sendMessage API method.
type SendMessagePayload struct {
	ChatID           string                `json:"chat_id"`
	Text             string                `json:"text"`
	ParseMode        string                `json:"parse_mode,omitempty"`
	ReplyToMessageID int                   `json:"reply_to_message_id,omitempty"`
	ReplyMarkup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	MessageThreadID  int                   `json:"message_thread_id,omitempty"`
	// DisableWebPreview 仅文本消息适用（媒体消息没有链接预览）。
	DisableWebPreview bool `json:"disable_web_page_preview,omitempty"`
	MessageOptions
}

// SendPhotoPayload is the JSON body for the sendPhoto API method.
// Photo can be a file_id, URL, or attach://<file_attach_name>.
type SendPhotoPayload struct {
	ChatID           string                `json:"chat_id"`
	Photo            string                `json:"photo"`
	Caption          string                `json:"caption,omitempty"`
	ParseMode        string                `json:"parse_mode,omitempty"`
	ReplyToMessageID int                   `json:"reply_to_message_id,omitempty"`
	ReplyMarkup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	MessageOptions
}

// SendAudioPayload is the JSON body for the sendAudio API method.
type SendAudioPayload struct {
	ChatID           string                `json:"chat_id"`
	Audio            string                `json:"audio"`
	Caption          string                `json:"caption,omitempty"`
	ParseMode        string                `json:"parse_mode,omitempty"`
	ReplyToMessageID int                   `json:"reply_to_message_id,omitempty"`
	ReplyMarkup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	MessageOptions
}

// SendVideoPayload is the JSON body for the sendVideo API method.
type SendVideoPayload struct {
	ChatID           string                `json:"chat_id"`
	Video            string                `json:"video"`
	Caption          string                `json:"caption,omitempty"`
	ParseMode        string                `json:"parse_mode,omitempty"`
	ReplyToMessageID int                   `json:"reply_to_message_id,omitempty"`
	ReplyMarkup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	MessageOptions
}

// SendDocumentPayload is the JSON body for the sendDocument API method.
type SendDocumentPayload struct {
	ChatID           string                `json:"chat_id"`
	Document         string                `json:"document"`
	Caption          string                `json:"caption,omitempty"`
	ParseMode        string                `json:"parse_mode,omitempty"`
	ReplyToMessageID int                   `json:"reply_to_message_id,omitempty"`
	ReplyMarkup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	MessageOptions
}

// EditMessageTextPayload is the JSON body for the editMessageText API method.
type EditMessageTextPayload struct {
	ChatID            string                `json:"chat_id,omitempty"`
	MessageID         int                   `json:"message_id,omitempty"`
	Text              string                `json:"text"`
	ParseMode         string                `json:"parse_mode,omitempty"`
	ReplyMarkup       *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	DisableWebPreview bool                  `json:"disable_web_page_preview,omitempty"`
}

// EditMessageReplyMarkupPayload is the JSON body for the editMessageReplyMarkup API method.
type EditMessageReplyMarkupPayload struct {
	ChatID      string                `json:"chat_id,omitempty"`
	MessageID   int                   `json:"message_id,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// DeleteMessagePayload is the JSON body for the deleteMessage API method.
type DeleteMessagePayload struct {
	ChatID    string `json:"chat_id"`
	MessageID int    `json:"message_id"`
}

// SendChatActionPayload is the JSON body for the sendChatAction API method.
type SendChatActionPayload struct {
	ChatID string `json:"chat_id"`
	Action string `json:"action"`
}

// SetMessageReactionPayload is the JSON body for the setMessageReaction API method.
type SetMessageReactionPayload struct {
	ChatID    string `json:"chat_id"`
	MessageID int    `json:"message_id"`
	Emoji     string `json:"emoji,omitempty"`
	IsBig     bool   `json:"is_big,omitempty"`
}

// AnswerCallbackQueryPayload is the JSON body for the answerCallbackQuery API method.
type AnswerCallbackQueryPayload struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
	URL             string `json:"url,omitempty"`
}

// GetUpdatesPayload is the JSON body for the getUpdates API method.
type GetUpdatesPayload struct {
	Offset  int `json:"offset,omitempty"`
	Timeout int `json:"timeout,omitempty"`
	Limit   int `json:"limit,omitempty"`
}

// GetFilePayload is the JSON body for the getFile API method.
type GetFilePayload struct {
	FileID string `json:"file_id"`
}

// File represents a file ready to be downloaded, as returned by getFile.
//
// FilePath 的有效期约为 1 小时，过期后需重新调用 getFile。
type File struct {
	// FileID is the identifier for this file.
	FileID string `json:"file_id"`
	// FileUniqueID is the permanent unique identifier for this file.
	FileUniqueID string `json:"file_unique_id"`
	// FileSize is the file size in bytes.
	FileSize int64 `json:"file_size,omitempty"`
	// FilePath is the relative path used to build the download URL.
	FilePath string `json:"file_path,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// Outgoing keyboard types
// ────────────────────────────────────────────────────────────────────────────

// InlineKeyboardMarkup represents an inline keyboard attached to a message.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton represents one button in an inline keyboard.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
	SwitchInline string `json:"switch_inline_query,omitempty"`
}

// FormatChatID converts a Telegram int64 chat ID to a string.
func FormatChatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
