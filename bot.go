package remilia

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

const (
	// DefaultShutdownTimeout is the default timeout for graceful shutdown
	DefaultShutdownTimeout = 30 * time.Second
)

// Bot 是对 Engine 的高级封装，提供完整的生命周期管理
type Bot struct {
	engine       *engine.Engine
	adapter      Adapter
	lifecycle    *lifecycle.Manager
	health       *health.Check
	config       *Config
	openAPI      openapi.OpenAPI // OpenAPI client for sending messages
	tokenManager *token.Manager  // Token manager for lifecycle management

	mu        sync.RWMutex
	running   bool
	starting  bool // Prevents concurrent Start() calls
	startTime time.Time
	stopTime  time.Time
}

// Config Bot 配置
type Config struct {
	Name    string
	Version string
	Debug   bool
}

// NewBot 创建新的 Bot 实例
func NewBot(adapter Adapter, engine *engine.Engine, opts ...Option) *Bot {
	// 验证必需参数
	if adapter == nil {
		logger.Panic("[Bot] adapter cannot be nil")
	}
	if engine == nil {
		logger.Panic("[Bot] engine cannot be nil")
	}

	b := &Bot{
		engine:    engine,
		adapter:   adapter,
		lifecycle: lifecycle.NewManager(),
		config: &Config{
			Name:    "remilia-bot",
			Version: "0.9.0",
			Debug:   false,
		},
	}

	// 应用选项
	for _, opt := range opts {
		opt(b)
	}

	// 初始化健康检查
	b.health = health.NewCheck()

	// 添加 Bot 自己的 checker
	b.health.AddChecker(NewBotStatusChecker(b))

	// 添加 Adapter checker
	if adapter != nil {
		b.health.AddChecker(NewAdapterHealthChecker(adapter))
	}

	// 添加 Engine checker
	if engine != nil {
		b.health.AddChecker(health.NewEngineHealthChecker(engine))
	}

	// 注册组件到生命周期管理器
	b.lifecycle.Register(lifecycle.NewSimpleComponent(
		"adapter",
		nil, // onStart
		func(ctx context.Context) error {
			// onRun: adapter.Start 是阻塞的，适合在这里运行
			return b.adapter.Start(ctx, b.handleEvent)
		},
		func(ctx context.Context) error {
			// onStop
			return b.adapter.Stop(ctx)
		},
	))

	b.lifecycle.Register(lifecycle.NewSimpleComponent(
		"engine",
		func(ctx context.Context) error {
			// onStart: Engine 初始化（如果需要）
			return nil
		},
		nil, // onRun: Engine 没有阻塞循环，使用默认行为（等待 ctx.Done）
		func(ctx context.Context) error {
			// onStop
			return b.engine.Shutdown(ctx)
		},
	))

	return b
}

// NewBotWithInfo 创建带 OpenAPI 支持的 Bot 实例
// 这个构造函数会自动初始化 OpenAPI client，用于发送消息
func NewBotWithInfo(adapter Adapter, engine *engine.Engine, botInfo *dto.BotInfo, opts ...Option) *Bot {
	b := NewBot(adapter, engine, opts...)

	// 初始化 OpenAPI client
	if botInfo != nil {
		tokenManager := token.NewManager(botInfo)
		b.tokenManager = tokenManager // 保存引用用于生命周期管理
		b.openAPI = openapi.New(tokenManager)

		// 添加 Token Manager health checker
		b.health.AddChecker(NewTokenManagerHealthChecker(b))

		logger.Info("[Bot] OpenAPI client initialized")
	} else {
		logger.Warn("[Bot] BotInfo is nil, OpenAPI client not initialized")
	}

	return b
}

// Start 启动 Bot
func (b *Bot) Start() error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		logger.Warn("[Bot] Already running")
		return nil
	}
	if b.starting {
		b.mu.Unlock()
		logger.Warn("[Bot] Already starting")
		return nil
	}
	b.starting = true
	b.mu.Unlock()

	// 使用 defer 确保在任何情况下都清理 starting 标志
	defer func() {
		// 只有在未成功启动时才清除 starting 标志
		b.mu.Lock()
		if !b.running {
			b.starting = false
		}
		b.mu.Unlock()
	}()

	logger.WithFields(logger.Fields{
		"name":    b.config.Name,
		"version": b.config.Version,
	}).Info("[Bot] Starting...")

	// 添加超时保护，防止永久阻塞
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 使用生命周期管理器启动所有组件
	if err := b.lifecycle.Start(ctx); err != nil {
		logger.WithError(err).Error("[Bot] Failed to start")
		return err
	}

	// 启动成功后设置状态
	b.mu.Lock()
	b.running = true
	b.startTime = time.Now()
	b.starting = false
	b.mu.Unlock()

	logger.Info("[Bot] Started successfully")
	return nil
}

// handleEvent 处理事件
func (b *Bot) handleEvent(payload *dto.Payload) {
	if b.config.Debug {
		logger.WithFields(logger.Fields{
			"type": payload.Type,
			"id":   payload.ID,
		}).Debug("[Bot] Event received")
	}

	// 安全检查：确保 openAPI client 已初始化
	api := b.openAPI
	if api == nil {
		logger.Warn("[Bot] OpenAPI client not initialized, event processing may fail")
		// 仍然继续处理，context 可以处理 nil API
	}

	// 创建 Context，传入 openAPI client
	ctx := eventctx.NewContext(payload, api)

	// 使用 Engine 处理事件
	b.engine.ProcessEvent(ctx)
}

// Stop 优雅关闭 Bot
func (b *Bot) Stop(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		logger.Warn("[Bot] Not running")
		return nil
	}
	b.running = false
	b.stopTime = time.Now()
	b.mu.Unlock()

	logger.Info("[Bot] Shutting down...")

	// 使用 channel 来处理异步关闭
	done := make(chan error, 1)
	go func() {
		// 使用生命周期管理器停止所有组件（逆序）
		err := b.lifecycle.Stop(ctx)

		// 停止 token manager（如果存在）
		if b.tokenManager != nil {
			logger.Debug("[Bot] Stopping token manager...")
			b.tokenManager.Stop()
		}

		done <- err
	}()

	// 等待关闭完成或超时
	select {
	case err := <-done:
		if err != nil {
			logger.WithError(err).Error("[Bot] Stop completed with errors")
			return err
		}
		logger.Info("[Bot] Stop complete")
		return nil
	case <-ctx.Done():
		logger.WithError(ctx.Err()).Warn("[Bot] Stop timeout exceeded")
		return ctx.Err()
	}
}

// Shutdown 使用默认超时时间优雅关闭 Bot
// 这是 Stop 的便捷包装，使用 DefaultShutdownTimeout 作为超时时间
func (b *Bot) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()
	return b.Stop(ctx)
}

// Engine 返回 Bot 的 Engine 实例
func (b *Bot) Engine() *engine.Engine {
	return b.engine
}

// GetEngine 返回 Bot 的 Engine 实例（别名）
func (b *Bot) GetEngine() *engine.Engine {
	return b.engine
}

// IsRunning 返回 Bot 是否正在运行
func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

// Uptime 返回 Bot 运行时间
func (b *Bot) Uptime() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.running {
		if !b.stopTime.IsZero() {
			return b.stopTime.Sub(b.startTime)
		}
		return 0
	}

	return time.Since(b.startTime)
}

// Config 获取配置
func (b *Bot) Config() *Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

// Health 健康检查
func (b *Bot) Health() health.CheckResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.health.Check(ctx)
}

// HealthCheck 返回 health.Check 实例（用于高级配置）
func (b *Bot) HealthCheck() *health.Check {
	return b.health
}

// State 返回生命周期状态
func (b *Bot) State() lifecycle.State {
	return b.lifecycle.State()
}

// OnAny 注册处理所有事件的规则（convenience method）
func (b *Bot) OnAny(rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnAny(rule...)
}

// OnC2C 注册处理私聊消息的规则（convenience method）
func (b *Bot) OnC2C(rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnC2C(rule...)
}

// OnGroupAt 注册处理群@消息的规则（convenience method）
func (b *Bot) OnGroupAt(rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnGroupAt(rule...)
}

// On 注册自定义规则（convenience method）
func (b *Bot) On(eventType dto.EventType, rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.On(eventType, rule...)
}

// WaitForShutdown 等待系统信号并优雅关闭
func (b *Bot) WaitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh

	logger.Info("[Bot] Received shutdown signal")

	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	if err := b.Stop(ctx); err != nil {
		logger.WithError(err).Error("[Bot] Shutdown failed")
	}
}
