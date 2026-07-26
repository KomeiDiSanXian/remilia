package telegram

// Test-only exports of unexported symbols.
//
// This file is compiled only during tests (files named *_test.go are excluded
// from production builds, but export_test.go has a _test.go suffix so it is
// only compiled for tests). The exports here allow integration_test.go to
// access package-internal functions without making them public API.

var (
	NewEvent              = newEvent
	BuildInlineKeyboard   = buildInlineKeyboard
	ExtensionFromMIME     = extensionFromMIME
	UserFromTelegram      = userFromTelegram
	ChatFromTelegram      = chatFromTelegram
	CollectAttachments    = collectAttachments
	ParseMessageID        = parseMessageID
	ExtractMessageID      = extractMessageID
	NewMessageEvent       = newMessageEvent
	NewCallbackQueryEvent = newCallbackQueryEvent
	NewChatMemberEvent    = newChatMemberEvent

	// For Client testing with a custom base URL
	SetClientBaseURL = func(c *Client, baseURL string) {
		c.baseURL = baseURL
	}

	// For adapter testing with a mock bot user
	SetAdapterBotUser = func(a *PollingAdapter, u *User) {
		a.botUser = u
	}
)

// NewTestAdapter creates an adapter with a test client and bot user, bypassing getMe.
func NewTestAdapter(cfg Config, client *Client, botUser *User) *PollingAdapter {
	return &PollingAdapter{
		cfg:     cfg,
		client:  client,
		sender:  newSender(client, botUser.UserName()),
		botUser: botUser,
	}
}
