// Package telegram implements the Telegram Bot API platform adapter for remilia.
//
// The adapter uses long polling (getUpdates) to receive events and wraps them
// into platform.Event for the framework core. No external Telegram SDK is
// required; all communication uses net/http to call the Telegram Bot API
// directly.
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

import (
	stdctx "context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// PollingAdapter is the Telegram platform.Adapter implementation using long polling.
//
// It connects to the Telegram Bot API via getUpdates long polling, wraps
// incoming updates into platform.Event instances, and dispatches them to the
// handler registered in Start(). The adapter is stoppable via context
// cancellation.
//
// PollingAdapter implements:
//   - platform.Adapter
//   - platform.BotIdentity (via GetMe response)
//   - platform.RecoverableAdapter (disconnect notification)
//   - platform.HealthDetailer (runtime health info)
type PollingAdapter struct {
	platform.DisconnectNotifier
	cfg    Config
	client *Client
	sender *telegramSender

	cancel   stdctx.CancelFunc
	mu       sync.RWMutex
	running  bool
	starting atomic.Bool
	botUser  *User
}

// Config holds Telegram connection parameters.
type Config struct {
	// Token is the Telegram Bot API token (required).
	// Obtain from @BotFather on Telegram.
	Token string

	// PollTimeout is the long polling timeout in seconds (default: 30).
	// Controls how long the getUpdates call waits before returning empty results.
	PollTimeout int
}

// NewAdapter creates a PollingAdapter with the given bot token.
//
// It immediately calls getMe to verify the token and fetch the bot's identity.
// Returns an error if the token is empty or the API call fails.
func NewAdapter(token string) (*PollingAdapter, error) {
	return NewPollingAdapter(Config{Token: token})
}

// NewPollingAdapter creates a PollingAdapter with full configuration.
//
// Validates the token (must be non-empty), applies defaults (PollTimeout=30),
// and calls getMe to verify connectivity and fetch bot user info.
func NewPollingAdapter(cfg Config) (*PollingAdapter, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("telegram adapter: token is required")
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 30
	}

	client := NewClient(cfg.Token)

	botUser, err := client.GetMe(stdctx.Background())
	if err != nil {
		return nil, fmt.Errorf("telegram adapter: getMe failed: %w", err)
	}

	adapter := &PollingAdapter{
		cfg:     cfg,
		client:  client,
		sender:  newSender(client, botUser.UserName()),
		botUser: botUser,
	}

	return adapter, nil
}

// Platform returns "telegram".
func (a *PollingAdapter) Platform() string { return PlatformID }

// Sender returns the Telegram message sender.
func (a *PollingAdapter) Sender() platform.Sender { return a.sender }

// Capabilities returns Telegram capabilities.
func (a *PollingAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        true,
		Buttons:         true,
		MultiAttachment: true,
		MessageEdit:     true,
		MessageDelete:   true,
		Embeds:          false,
		FileUpload:      true,
		GuildSupport:    false,
		Reactions:       true,
		ThreadReply:     true,
		TypingIndicator: true,
		MentionAll:      false,
		VoiceChannel:    false,
		MaxTextLength:   4096,
		MaxAttachmentMB: 50,
	}
}

// IsRunning returns true if the polling loop is active.
func (a *PollingAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start begins long-polling for Telegram updates.
//
// The method blocks until ctx is canceled or a non-recoverable error occurs.
// For each incoming Update, it creates a platform.Event via newEvent and
// dispatches it to handler using platform.SafeDispatch.
//
// Disconnects (getUpdates errors) are logged and sent to disconnect listeners
// registered via OnDisconnect.
//
// Only one Start call is effective; subsequent calls return nil immediately.
func (a *PollingAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
	}
	defer a.starting.Store(false)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	cancelCtx, cancel := stdctx.WithCancel(ctx)
	a.cancel = cancel
	a.running = true
	a.mu.Unlock()

	logger.Infof("[telegram.PollingAdapter] Started polling (bot: %s)", a.botUser.UserName())

	var offset int

	for {
		select {
		case <-cancelCtx.Done():
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
			logger.Debug("[telegram.PollingAdapter] Polling loop stopped")
			return nil
		default:
		}

		updates, err := a.client.GetUpdates(cancelCtx, offset, a.cfg.PollTimeout, 100)
		if err != nil {
			if cancelCtx.Err() != nil {
				continue
			}
			logger.WithError(err).Warn("[telegram.PollingAdapter] getUpdates error")
			a.NotifyDisconnect(fmt.Errorf("telegram: getUpdates error: %w", err))
			time.Sleep(3 * time.Second)
			continue
		}

		for _, upd := range updates {
			event := newEvent(&upd)
			if event != nil {
				platform.SafeDispatch(handler, event)
			}
			offset = int(upd.UpdateID) + 1
		}
	}
}

// Stop gracefully stops the polling loop.
//
// Cancels the internal context which causes Start() to exit its loop.
// The call does not wait for the polling goroutine to finish.
func (a *PollingAdapter) Stop(_ stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// ── platform.BotIdentity ────────────────────────────────────────────────────

// BotID returns the bot's numeric ID as a string.
//
// The ID is fetched from the getMe response during adapter initialization.
// Returns an empty string if not yet connected.
func (a *PollingAdapter) BotID() string {
	if a.botUser == nil {
		return ""
	}
	return fmt.Sprintf("%d", a.botUser.ID)
}

// BotName returns the bot's @username.
//
// Falls back to first name if the bot has no username set.
// Returns an empty string if not yet connected.
func (a *PollingAdapter) BotName() string {
	if a.botUser == nil {
		return ""
	}
	return a.botUser.UserName()
}

// ── platform.HealthDetailer ──────────────────────────────────────────────────

// HealthDetail returns runtime health information about the Telegram adapter.
func (a *PollingAdapter) HealthDetail() map[string]any {
	detail := map[string]any{
		"connection": "long_polling",
	}
	if a.botUser != nil {
		detail["bot_username"] = a.botUser.UserName()
	}
	detail["polling_active"] = a.IsRunning()
	return detail
}

// compile-time interface checks
var (
	_ platform.Adapter            = (*PollingAdapter)(nil)
	_ platform.BotIdentity        = (*PollingAdapter)(nil)
	_ platform.RecoverableAdapter = (*PollingAdapter)(nil)
	_ platform.HealthDetailer     = (*PollingAdapter)(nil)
)
