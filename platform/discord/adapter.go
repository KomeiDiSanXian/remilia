// Package discord is the Discord platform.Adapter implementation.
//
// See config.go for connection method documentation.
package discord

import (
	stdctx "context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
)

// PlatformID is the unique identifier for the Discord platform.
const PlatformID = "discord"

// discordCapabilities 返回 Discord 平台的能力声明。
//
// 使用函数而非包级变量，保留运行时动态更新的能力（如 Nitro 附件大小上限）。
func discordCapabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        true,
		Buttons:         true,
		MultiAttachment: true,
		MessageEdit:     true,
		MessageDelete:   true,
		Embeds:          true,
		FileUpload:      true,
		GuildSupport:    true,
		Reactions:       true,
		ThreadReply:     true,
		TypingIndicator: true,
		MentionAll:      true,
		VoiceChannel:    true,
		// Discord 量化限制（免费账号保守值）
		MaxTextLength:    2000, // Discord 普通消息文本上限
		MaxAttachmentMB:  8,    // 免费账号单文件上限（Nitro 为 50/500 MB）
		MaxButtonsPerRow: 5,    // Discord 组件行最多 5 个按钮
		MaxButtonRows:    5,    // Discord 消息最多 5 行组件
		MaxEmbedFields:   25,   // Discord Embed 最多 25 个 fields
	}
}

// ────────────────────────────────────────────────────────────────────────────
// GatewayAdapter
// ────────────────────────────────────────────────────────────────────────────

// GatewayAdapter is the Discord platform.Adapter implementation that connects
// via the Discord Gateway (persistent WebSocket).
//
// Usage:
//
//	adapter, err := discord.NewAdapter("BOT_TOKEN")
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
type GatewayAdapter struct {
	platform.DisconnectNotifier
	config  GatewayConfig
	session *discordgo.Session
	sender  *discordSender

	ctx      stdctx.Context //nolint:unused
	cancel   stdctx.CancelFunc
	wg       sync.WaitGroup
	mu       sync.RWMutex
	running  bool
	starting atomic.Bool
}

// NewAdapter creates a GatewayAdapter with default configuration for the given token.
func NewAdapter(token string) (*GatewayAdapter, error) {
	return NewGatewayAdapter(DefaultGatewayConfig(token))
}

// NewGatewayAdapter creates a GatewayAdapter with the provided configuration.
func NewGatewayAdapter(cfg GatewayConfig) (*GatewayAdapter, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("discord gateway: failed to create session: %w", err)
	}

	intents := cfg.Intents
	if intents == 0 {
		intents = DefaultIntents
	}
	session.Identify.Intents = intents
	session.ShouldReconnectOnError = cfg.ShouldReconnect
	session.StateEnabled = true

	if cfg.NumShards > 1 {
		session.ShardID = cfg.ShardID
		session.ShardCount = cfg.NumShards
	}
	if cfg.LargeThreshold > 0 {
		session.Identify.LargeThreshold = cfg.LargeThreshold
	}

	logger.Infof("[discord.GatewayAdapter] Creating adapter: intents=%d",
		cfg.Intents)

	return &GatewayAdapter{
		config:  cfg,
		session: session,
		sender:  newSender(session),
	}, nil
}

// Platform returns "discord".
func (a *GatewayAdapter) Platform() string { return PlatformID }

// Sender returns the Discord message sender.
func (a *GatewayAdapter) Sender() platform.Sender { return a.sender }

// Capabilities returns Discord platform feature capabilities.
func (a *GatewayAdapter) Capabilities() platform.Capabilities { return discordCapabilities() }

// IsRunning returns true if the Gateway connection is active.
func (a *GatewayAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start opens the Discord Gateway connection and begins processing events.
// Blocks until ctx is canceled.
func (a *GatewayAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
	}
	defer a.starting.Store(false)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}

	bufSize := a.config.EventBufferSize
	if bufSize <= 0 {
		bufSize = 100
	}
	eventCh := make(chan platform.Event, bufSize)
	cancelCtx, cancel := stdctx.WithCancel(ctx)
	a.cancel = cancel
	a.running = true
	a.mu.Unlock()

	a.registerHandlers(cancelCtx, eventCh)

	a.session.AddHandler(func(s *discordgo.Session, d *discordgo.Disconnect) {
		logger.Warn("[discord.GatewayAdapter] Disconnected from Gateway")
		a.NotifyDisconnect(fmt.Errorf("discord: gateway disconnected"))
	})

	if err := a.session.Open(); err != nil {
		cancel()
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		close(eventCh)
		return fmt.Errorf("discord gateway: failed to open connection: %w", err)
	}

	logger.Infof("[discord.GatewayAdapter] Gateway connected, intents=%d", a.config.Intents)

	// 事件分发 goroutine：从 eventCh 读取事件，直接调用 handler。
	// 并发控制由 Engine 的 ExecPool 负责。
	a.wg.Go(func() {
		for {
			select {
			case event, ok := <-eventCh:
				if !ok {
					return
				}
				safeInvoke(handler, event)
			case <-cancelCtx.Done():
				return
			}
		}
	})

	<-cancelCtx.Done()
	close(eventCh)
	a.wg.Wait()

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()

	logger.Debug("[discord.GatewayAdapter] Event loop stopped")
	return nil
}

// Stop closes the Discord Gateway connection gracefully.
func (a *GatewayAdapter) Stop(_ stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	// 停止 sender 的后台 cleanup goroutine
	if a.sender != nil {
		a.sender.stopCleanup()
	}

	if err := a.session.Close(); err != nil {
		return fmt.Errorf("discord gateway: error closing session: %w", err)
	}
	return nil
}

// BotID implements platform.BotIdentity — returns bot's Discord user ID.
func (a *GatewayAdapter) BotID() string {
	if a.session.State != nil && a.session.State.User != nil {
		return a.session.State.User.ID
	}
	return ""
}

// BotName implements platform.BotIdentity — returns bot's Discord username.
func (a *GatewayAdapter) BotName() string {
	if a.session.State != nil && a.session.State.User != nil {
		return a.session.State.User.Username
	}
	return ""
}

// ── platform.HealthDetailer ──────────────────────────────────────────────────

func (a *GatewayAdapter) HealthDetail() map[string]any {
	detail := map[string]any{
		"connection": "gateway_websocket",
	}
	if a.session != nil && a.session.State != nil && a.session.State.User != nil {
		detail["gateway_connected"] = true
	}
	return detail
}

// 编译期接口断言
var (
	_ platform.Adapter            = (*GatewayAdapter)(nil)
	_ platform.BotIdentity        = (*GatewayAdapter)(nil)
	_ platform.RecoverableAdapter = (*GatewayAdapter)(nil)
	_ platform.HealthDetailer     = (*GatewayAdapter)(nil)
)

// Session returns the underlying *discordgo.Session for advanced operations
// such as registering slash commands or joining voice channels.
//
// Do not call Open()/Close() directly; use Start()/Stop() instead.
func (a *GatewayAdapter) Session() *discordgo.Session {
	return a.session
}

// ────────────────────────────────────────────────────────────────────────────
// Event handler registration
// ────────────────────────────────────────────────────────────────────────────

func (a *GatewayAdapter) registerHandlers(ctx stdctx.Context, eventCh chan<- platform.Event) {
	send := func(e platform.Event) {
		select {
		case eventCh <- e:
		case <-ctx.Done():
		}
	}

	// Message events
	a.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		send(NewMessageCreateEvent(m))
	})
	a.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		send(NewMessageUpdateEvent(m))
	})
	a.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageDelete) {
		send(NewMessageDeleteEvent(m))
	})

	// Interaction events — store the interaction object before dispatching
	// so the sender can respond via the Interactions API.
	a.session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		a.sender.storeInteraction(i.Interaction)
		send(NewInteractionCreateEvent(i))
	})

	// Guild lifecycle
	a.session.AddHandler(func(s *discordgo.Session, g *discordgo.GuildCreate) {
		send(NewGuildCreateEvent(g))
	})
	a.session.AddHandler(func(s *discordgo.Session, g *discordgo.GuildDelete) {
		send(NewGuildDeleteEvent(g))
	})
	a.session.AddHandler(func(s *discordgo.Session, g *discordgo.GuildUpdate) {
		send(NewGuildUpdateEvent(g))
	})

	// Guild members
	a.session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		send(NewGuildMemberAddEvent(m))
	})
	a.session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
		send(NewGuildMemberRemoveEvent(m))
	})
	a.session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
		send(NewGuildMemberUpdateEvent(m))
	})

	// Reactions
	a.session.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		send(NewMessageReactionAddEvent(r))
	})
	a.session.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
		send(NewMessageReactionRemoveEvent(r))
	})

	// Channels
	a.session.AddHandler(func(s *discordgo.Session, c *discordgo.ChannelCreate) {
		send(NewChannelCreateEvent(c))
	})
	a.session.AddHandler(func(s *discordgo.Session, c *discordgo.ChannelUpdate) {
		send(NewChannelUpdateEvent(c))
	})
	a.session.AddHandler(func(s *discordgo.Session, c *discordgo.ChannelDelete) {
		send(NewChannelDeleteEvent(c))
	})

	// System events
	a.session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		logger.Infof("[discord.GatewayAdapter] Ready: logged in as %s#%s (ID: %s)",
			r.User.Username, r.User.Discriminator, r.User.ID)
		send(NewReadyEvent(r))
	})
	a.session.AddHandler(func(s *discordgo.Session, r *discordgo.Resumed) {
		logger.Info("[discord.GatewayAdapter] Connection resumed")
		send(NewResumedEvent(r))
	})
}

// safeInvoke calls handler, recovering from any panics to prevent worker crashes.
func safeInvoke(handler func(platform.Event), event platform.Event) {
	platform.SafeDispatch(handler, event)
}
