// Package discord is the Discord platform.Adapter implementation.
//
// See config.go for connection method documentation.
package discord

import (
	stdctx "context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
)

// PlatformID is the unique identifier for the Discord platform.
const PlatformID = "discord"

// Capabilities declares the feature set supported by Discord.
var Capabilities = platform.Capabilities{
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
	session *discordgo.Session
	sender  *discordSender
	config  GatewayConfig
	workers int

	mu      sync.RWMutex
	running bool
	cancel  stdctx.CancelFunc
	wg      sync.WaitGroup

	starting atomic.Bool

	disconnectMu  sync.Mutex
	disconnectFns []func(error)
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

	workers := cfg.WorkerCount
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	return &GatewayAdapter{
		session: session,
		sender:  newSender(session),
		config:  cfg,
		workers: workers,
	}, nil
}

// Platform returns "discord".
func (a *GatewayAdapter) Platform() string { return PlatformID }

// Sender returns the Discord message sender.
func (a *GatewayAdapter) Sender() platform.Sender { return a.sender }

// Capabilities returns Discord platform feature capabilities.
func (a *GatewayAdapter) Capabilities() platform.Capabilities { return Capabilities }

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
		a.notifyDisconnect(fmt.Errorf("discord: gateway disconnected"))
	})

	if err := a.session.Open(); err != nil {
		cancel()
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		close(eventCh)
		return fmt.Errorf("discord gateway: failed to open connection: %w", err)
	}

	logger.Infof("[discord.GatewayAdapter] Gateway connected (workers=%d, intents=%d)",
		a.workers, a.config.Intents)

	workCh := make(chan platform.Event, a.workers*2)
	for i := 0; i < a.workers; i++ {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for event := range workCh {
				safeInvoke(handler, event)
			}
		}()
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer close(workCh)
		for {
			select {
			case event, ok := <-eventCh:
				if !ok {
					return
				}
				select {
				case workCh <- event:
				case <-cancelCtx.Done():
					return
				}
			case <-cancelCtx.Done():
				return
			}
		}
	}()

	<-cancelCtx.Done()
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

// OnDisconnect implements platform.RecoverableAdapter.
func (a *GatewayAdapter) OnDisconnect(fn func(error)) (unregister func()) {
	if fn == nil {
		return func() {}
	}
	a.disconnectMu.Lock()
	idx := len(a.disconnectFns)
	a.disconnectFns = append(a.disconnectFns, fn)
	a.disconnectMu.Unlock()
	return func() {
		a.disconnectMu.Lock()
		defer a.disconnectMu.Unlock()
		if idx < len(a.disconnectFns) {
			a.disconnectFns[idx] = nil
		}
	}
}

func (a *GatewayAdapter) notifyDisconnect(err error) {
	a.disconnectMu.Lock()
	fns := make([]func(error), len(a.disconnectFns))
	copy(fns, a.disconnectFns)
	a.disconnectMu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn(err)
		}
	}
}

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
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[discord] panic in event handler: %v", r)
		}
	}()
	handler(event)
}
