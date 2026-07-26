package telegram

import "github.com/KomeiDiSanXian/remilia/platform"

// Token keys stored in platform.ChatInfo.Tokens for sender routing.
const (
	// TokenCallbackID is the Telegram callback_query ID.
	//
	// The sender reads this from ChatInfo.Tokens to call answerCallbackQuery
	// when replying to an INTERACTION-kind event that originated from a
	// Telegram inline keyboard callback.
	TokenCallbackID = "callback_id"
)

// InlineButtonExtra carries Telegram-specific inline button fields.
//
// Attach to platform.Button.Extra. The telegramSender reads these via type
// assertion when building the InlineKeyboardMarkup.
//
// Example:
//
//	button := platform.Button{
//	    ID:    "my_data",
//	    Label: "Search",
//	    Extra: &telegram.InlineButtonExtra{
//	        SwitchInlineQuery: "query_text",
//	    },
//	}
type InlineButtonExtra struct {
	// SwitchInlineQuery prompts the user to select a chat and inserts the
	// bot's @username and the specified value into the input field.
	SwitchInlineQuery string `json:"switch_inline_query,omitempty"`
	// SwitchInlineQueryChosen is similar but allows specifying a chat type
	// filter (Telegram Bot API 7.0+).
	SwitchInlineQueryChosen string `json:"switch_inline_query_chosen,omitempty"`
}

// telegramExtraKey is the private key used to store Telegram-specific
// options inside platform.OutboundMessage.Extra.
const telegramExtraKey = "__telegram_message_extra__"

// MessageExtra holds Telegram-specific message send options.
//
// Inject into an OutboundMessage using ApplyExtra:
//
//	msg := platform.TextMessage("Hello").
//	    Then(telegram.ApplyExtra(telegram.MessageExtra{DisableNotification: true}))
type MessageExtra struct {
	// DisableWebPreview disables link previews for links in this message.
	DisableWebPreview bool `json:"disable_web_page_preview,omitempty"`
	// DisableNotification sends the message silently.
	DisableNotification bool `json:"disable_notification,omitempty"`
	// ProtectContent protects the message from being forwarded.
	ProtectContent bool `json:"protect_content,omitempty"`
	// AllowPaidBroadcast allows the message to be sent as a paid broadcast
	// (Telegram Stars; Bot API 7.10+).
	AllowPaidBroadcast bool `json:"allow_paid_broadcast,omitempty"`
}

// ApplyExtra injects Telegram-specific options into an OutboundMessage.
//
// Returns a new message; the original is not modified.
//
// Example:
//
//	msg := platform.TextMessage("Hello")
//	msg = telegram.ApplyExtra(msg, telegram.MessageExtra{DisableNotification: true})
func ApplyExtra(msg platform.OutboundMessage, extra MessageExtra) platform.OutboundMessage {
	return msg.WithExtra(telegramExtraKey, extra)
}

// extractExtra retrieves Telegram-specific options from an OutboundMessage.
//
// Returns zero-value MessageExtra if no extra was injected or the type
// does not match.
func extractExtra(msg platform.OutboundMessage) MessageExtra {
	if msg.Extra == nil {
		return MessageExtra{}
	}
	v, ok := msg.Extra[telegramExtraKey]
	if !ok {
		return MessageExtra{}
	}
	e, _ := v.(MessageExtra)
	return e
}
