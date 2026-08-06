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
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

const (
	// defaultPollTimeout 是 getUpdates 长轮询的默认挂起时长（秒）。
	defaultPollTimeout = 30
	// maxPollTimeout 是允许配置的长轮询上限（秒）。
	// 取值需为 HTTP 客户端超时留出余量，见 NewPollingAdapter 的说明。
	maxPollTimeout = 50
	// httpTimeoutMargin 是 HTTP 客户端超时相对长轮询时长的余量。
	httpTimeoutMargin = 15 * time.Second
	// fileResolveTimeout 是单次 getFile 解析附件地址的超时。
	fileResolveTimeout = 5 * time.Second
	// attachmentResolveBudget 是每批 getUpdates 用于解析附件地址的总时间预算。
	// 超出后剩余附件的 URL 留空，改由插件通过 Client.DownloadFile 按需获取。
	attachmentResolveBudget = 15 * time.Second
	// pollRetryDelay 是 getUpdates 出错后的重试间隔。
	pollRetryDelay = 3 * time.Second
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
		cfg.PollTimeout = defaultPollTimeout
	}
	// 长轮询时长必须留在 HTTP 客户端超时之内。
	//
	// getUpdates 在空闲会话里会被服务端挂满 PollTimeout 秒才返回，而客户端的
	// http.Client.Timeout 覆盖的是整个请求。二者不设约束时（例如
	// PollTimeout=60 撞上 60s 的客户端超时），客户端会先一步超时中止，
	// 于是一个完全健康的机器人每 60 多秒就上报一次"断线"，永远刷警告。
	if cfg.PollTimeout > maxPollTimeout {
		logger.Warnf("[telegram.PollingAdapter] PollTimeout=%ds 超出上限，已收敛为 %ds",
			cfg.PollTimeout, maxPollTimeout)
		cfg.PollTimeout = maxPollTimeout
	}

	// 客户端超时按轮询时长推算，留出网络往返余量。
	client := NewClientWithTimeout(cfg.Token,
		time.Duration(cfg.PollTimeout)*time.Second+httpTimeoutMargin)

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
		Caption:         true, // 媒体消息支持 caption 文本
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
			// 用可中断的等待代替 time.Sleep：后者不响应取消，
			// 会让 Stop 平白多等最多 3 秒。
			select {
			case <-time.After(pollRetryDelay):
			case <-cancelCtx.Done():
				continue
			}
			continue
		}

		// 每批更新共享一个附件解析预算。
		//
		// getUpdates 一次最多返回 100 条，而每个附件都要一次 getFile 往返。
		// 若不设总预算，Telegram API 抖动时整批更新会被串行的 getFile 拖住，
		// 期间所有会话的事件都无法投递，也不会发起新的 getUpdates——
		// 一个慢 getFile 就能让整个机器人停摆。
		budget := attachmentResolveBudget

		for _, upd := range updates {
			event := newEventWithBot(&upd, fmt.Sprintf("%d", a.botUser.ID))
			if event != nil {
				budget -= a.resolveAttachmentURLs(cancelCtx, event, budget)
				platform.SafeDispatch(handler, event)
			}
			offset = int(upd.UpdateID) + 1
		}
	}
}

// resolveAttachmentURLs 把入站附件的 file_id 换成可直接下载的 URL。
//
// Telegram 的 update 里只有不透明的 file_id，必须额外调用一次 getFile 才能
// 得到 file_path。这一步放在适配器里做（而不是 collectAttachments 里），
// 因为只有这里同时拿得到 Client 和 ctx。
//
// 解析失败不影响事件投递：URL 保持为空，插件仍可通过
// Attachment.Extra 中的 *FileMeta 拿到 file_id 自行处理。
//
// 安全提示：生成的 URL 路径中嵌有 bot token，属于可直接调用 API 的活凭据。
// 插件不应将其写入日志或转发给不受信任的下游；只需读取内容时，
// 优先使用 Client.DownloadFile。
// budget 是本批更新剩余的解析时间预算，返回本次实际消耗的时长。
// 预算耗尽时直接跳过解析（附件 URL 留空），保证事件投递不被阻塞。
func (a *PollingAdapter) resolveAttachmentURLs(ctx stdctx.Context, event platform.Event, budget time.Duration) time.Duration {
	// Attachments() 返回的是事件内部切片本身，逐元素改写即可生效。
	atts := event.Attachments()
	if len(atts) == 0 {
		return 0
	}

	started := time.Now()
	for i := range atts {
		meta, ok := atts[i].Extra[ExtraKeyFile].(*FileMeta)
		if !ok || meta == nil || meta.FileID == "" || atts[i].URL != "" {
			continue
		}
		spent := time.Since(started)
		if spent >= budget {
			// 只在预算刚耗尽时提示一次，避免同一批更新刷出上百条相同告警。
			if budget > 0 {
				logger.Warn("[telegram.PollingAdapter] 附件解析预算耗尽，剩余附件 URL 留空（可用 Client.DownloadFile 按需下载）")
			}
			break
		}

		timeout := min(fileResolveTimeout, budget-spent)
		fetchCtx, cancel := stdctx.WithTimeout(ctx, timeout)
		f, err := a.client.GetFile(fetchCtx, meta.FileID)
		cancel()
		if err != nil {
			logger.WithError(err).
				Debug("[telegram.PollingAdapter] getFile 失败，附件 URL 留空")
			continue
		}
		atts[i].URL = a.client.FileURL(f.FilePath)
		if atts[i].Size == 0 && f.FileSize > 0 && f.FileSize <= math.MaxInt32 {
			atts[i].Size = int(f.FileSize)
		}
	}
	return time.Since(started)
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
