// Package discord is the Discord platform.Adapter implementation.
//
// See config.go for connection method documentation.
package discord

import (
	stdctx "context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

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
		Caption:         true, // content 文本 + 附件同发
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
	cfg.setDefaults()

	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("discord gateway: failed to create session: %w", err)
	}

	session.Identify.Intents = cfg.Intents
	session.ShouldReconnectOnError = cfg.ShouldReconnect
	session.StateEnabled = true
	// 启用消息状态缓存：引用回查（fetchReferencedMessage）优先命中本地缓存，
	// 避免对"已删除/无权限"的引用发起必然 404 的 REST 调用。上限 100 条/频道，
	// 权衡内存占用（discordgo 默认 MaxMessageCount=0 时不缓存任何消息）。
	session.State.MaxMessageCount = 100

	if cfg.NumShards > 1 {
		session.ShardID = cfg.ShardID
		session.ShardCount = cfg.NumShards
	}
	session.Identify.LargeThreshold = cfg.LargeThreshold

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

	eventCh := make(chan platform.Event, a.config.EventBufferSize)
	cancelCtx, cancel := stdctx.WithCancel(ctx)
	a.cancel = cancel
	a.running = true
	a.mu.Unlock()

	// 重新拉起 interaction 缓存的清理协程（Stop 会停掉它）。
	// Start 明确支持被再次调用（Open 失败重试、Bot 热重启），
	// 缺少这一步的话，重启之后 sender.pending 就再无任何东西回收。
	if a.sender != nil {
		a.sender.startCleanup()
	}

	removers := a.registerHandlers(cancelCtx, eventCh)

	removers = append(removers, a.session.AddHandler(func(s *discordgo.Session, d *discordgo.Disconnect) {
		logger.Warn("[discord.GatewayAdapter] Disconnected from Gateway")
		a.NotifyDisconnect(fmt.Errorf("discord: gateway disconnected"))
	}))

	// 返回前注销本次注册的所有 handler：Start 可被再次调用（Open 失败重试、
	// Bot 热重启），不注销会造成 handler 叠加、同一事件被重复分发。
	defer func() {
		for _, rm := range removers {
			rm()
		}
	}()

	if err := a.session.Open(); err != nil {
		cancel()
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
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
				platform.SafeDispatch(handler, event)
			case <-cancelCtx.Done():
				return
			}
		}
	})

	<-cancelCtx.Done()
	// 不能 close(eventCh)：discordgo 在独立 goroutine 中调用 handler，
	// 此刻可能正有 handler 停在 send() 的 select 上。select 在多个 case 同时就绪时
	// 随机选择，因此即使 ctx 已取消，仍可能选中"向已关闭 channel 发送"而 panic，
	// 且该 panic 位于 discordgo 的 goroutine 中无人 recover，会直接终止进程。
	// 分发 goroutine 已由 cancelCtx.Done() 退出，channel 交给 GC 即可。
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

// quotedFetchTimeout 回查被引用消息（ChannelMessage）的单次超时。
//
// 包级变量而非常量，便于测试缩短。注意 discordgo 的 ChannelMessage 不接收
// context：超时只放弃等待，底层 REST 请求仍会在后台完成——因此并发回查
// 必须用 quotedFetchSem 限制数量，防止限流/慢网络下 goroutine 无界堆积。
var quotedFetchTimeout = 3 * time.Second

// quotedFetchSem 限制并发引用回查数量（Discord 全局限流 50 req/s）。
var quotedFetchSem = make(chan struct{}, 4)

// fetchChannelMessage 是 ChannelMessage 回查的可注入实现（测试替身点）。
var fetchChannelMessage = func(s *discordgo.Session, channelID, messageID string) (*discordgo.Message, error) {
	return s.ChannelMessage(channelID, messageID)
}

// fetchReferencedMessage 尽力回查缺失的被引用消息。
//
// Discord 的 reply payload 通常内嵌 referenced_message；消息已删除、缓存
// 过期或部分跨频道引用时缺省。此时回查一次 ChannelMessage 补齐，使引用段
// 能携带被引用附件（buildDiscordSegments 填充 Extra[SegmentExtraQuoteAtts]）。
//
// 顺序：本地状态缓存（网关已缓存近期消息，零网络开销）→ 受并发上限约束的
// REST 回查。被引用消息已删除或无权限时 ChannelMessage 必然 404，属白耗
// 调用——状态缓存命中失败可部分规避（已删除消息通常不在缓存中）。
// 失败、超时或并发信号量已满时静默跳过，不影响事件投递。
func fetchReferencedMessage(s *discordgo.Session, m *discordgo.Message) {
	if s == nil || m == nil || m.MessageReference == nil || m.ReferencedMessage != nil {
		return
	}
	if m.MessageReference.MessageID == "" {
		return
	}

	// 1) 本地状态缓存：命中则无需网络调用
	if s.State != nil {
		if cached, err := s.State.Message(m.ChannelID, m.MessageReference.MessageID); err == nil && cached != nil {
			m.ReferencedMessage = cached
			return
		}
	}

	// 2) REST 回查：并发上限已满时直接跳过（尽力而为，不排队）
	select {
	case quotedFetchSem <- struct{}{}:
	default:
		return
	}
	defer func() { <-quotedFetchSem }()

	type fetchResult struct {
		msg *discordgo.Message
		err error
	}
	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), quotedFetchTimeout)
	defer cancel()
	ch := make(chan fetchResult, 1)
	go func() {
		msg, err := fetchChannelMessage(s, m.ChannelID, m.MessageReference.MessageID)
		ch <- fetchResult{msg: msg, err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			logger.WithError(r.err).Debug("[discord] 回查被引用消息失败")
			return
		}
		m.ReferencedMessage = r.msg
	case <-ctx.Done():
		logger.Debug("[discord] 回查被引用消息超时")
	}
}

func (a *GatewayAdapter) registerHandlers(ctx stdctx.Context, eventCh chan<- platform.Event) []func() {
	send := func(e platform.Event) {
		select {
		case eventCh <- e:
		case <-ctx.Done():
		}
	}

	// 收集 AddHandler 返回的注销函数：Start 可能被再次调用（重连/重启），
	// 不注销旧 handler 会导致同一事件被重复投递，且旧闭包仍指向已废弃的 eventCh。
	var removers []func()
	add := func(h any) {
		removers = append(removers, a.session.AddHandler(h))
	}

	// Message events
	add(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		fetchReferencedMessage(s, m.Message)
		send(NewMessageCreateEventWithBot(m, a.BotID()))
	})
	// 编辑事件不做引用回查：Discord 编辑事件通常已内嵌 referenced_message，
	// 且编辑频率远高于创建（链接预览等也会触发），逐条回查成本过高。
	add(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		send(NewMessageUpdateEventWithBot(m, a.BotID()))
	})
	add(func(s *discordgo.Session, m *discordgo.MessageDelete) {
		send(NewMessageDeleteEvent(m))
	})

	// Interaction events — store the interaction object before dispatching
	// so the sender can respond via the Interactions API.
	add(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		a.sender.storeInteraction(i.Interaction)
		send(NewInteractionCreateEvent(i))
	})

	// Guild lifecycle
	add(func(s *discordgo.Session, g *discordgo.GuildCreate) {
		send(NewGuildCreateEvent(g))
	})
	add(func(s *discordgo.Session, g *discordgo.GuildDelete) {
		send(NewGuildDeleteEvent(g))
	})
	add(func(s *discordgo.Session, g *discordgo.GuildUpdate) {
		send(NewGuildUpdateEvent(g))
	})

	// Guild members
	add(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		send(NewGuildMemberAddEvent(m))
	})
	add(func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
		send(NewGuildMemberRemoveEvent(m))
	})
	add(func(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
		send(NewGuildMemberUpdateEvent(m))
	})

	// Reactions
	add(func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		send(NewMessageReactionAddEvent(r))
	})
	add(func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
		send(NewMessageReactionRemoveEvent(r))
	})

	// Channels
	add(func(s *discordgo.Session, c *discordgo.ChannelCreate) {
		send(NewChannelCreateEvent(c))
	})
	add(func(s *discordgo.Session, c *discordgo.ChannelUpdate) {
		send(NewChannelUpdateEvent(c))
	})
	add(func(s *discordgo.Session, c *discordgo.ChannelDelete) {
		send(NewChannelDeleteEvent(c))
	})

	// System events
	add(func(s *discordgo.Session, r *discordgo.Ready) {
		logger.Infof("[discord.GatewayAdapter] Ready: logged in as %s#%s (ID: %s)",
			r.User.Username, r.User.Discriminator, r.User.ID)
		send(NewReadyEvent(r))
	})
	add(func(s *discordgo.Session, r *discordgo.Resumed) {
		logger.Info("[discord.GatewayAdapter] Connection resumed")
		send(NewResumedEvent(r))
	})

	return removers
}
