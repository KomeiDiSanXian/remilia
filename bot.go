package remilia

import (
	"context"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
)

// Bot 是对 Engine 的高级封装，提供完整的生命周期管理
type Bot struct {
	engine    *engine.Engine
	adapter   Adapter
	lifecycle *lifecycle.Manager
	health    *HealthChecker
	config    *Config

	mu        sync.RWMutex
	running   bool
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
	b.health = NewHealthChecker(b)

	// 注册组件到生命周期管理器
	b.lifecycle.Register(lifecycle.NewSimpleComponent(
		"adapter",
		func(ctx context.Context) error {
			return b.adapter.Start(ctx, b.handleEvent)
		},
		func(ctx context.Context) error {
			return b.adapter.Stop(ctx)
		},
	))

	b.lifecycle.Register(lifecycle.NewSimpleComponent(
		"engine",
		func(ctx context.Context) error {
			// Engine 通常不需要特殊启动
			return nil
		},
		func(ctx context.Context) error {
			return b.engine.Shutdown(ctx)
		},
	))

	return b
}

// Start 启动 Bot
func (b *Bot) Start() error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		logrus.Warn("[Bot] Already running")
		return nil
	}
	b.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"name":    b.config.Name,
		"version": b.config.Version,
	}).Info("[Bot] Starting...")

	// 使用生命周期管理器启动所有组件
	ctx := context.Background()
	if err := b.lifecycle.Start(ctx); err != nil {
		logrus.WithError(err).Error("[Bot] Failed to start")
		return err
	}

	// 启动成功后才设置状态，避免状态不一致
	b.mu.Lock()
	b.running = true
	b.startTime = time.Now()
	b.mu.Unlock()

	logrus.Info("[Bot] Started successfully")
	return nil
}

// handleEvent 处理事件
func (b *Bot) handleEvent(payload *dto.Payload) {
	if b.config.Debug {
		logrus.WithFields(logrus.Fields{
			"type": payload.Type,
			"id":   payload.ID,
		}).Debug("[Bot] Event received")
	}

	// 创建 Context
	ctx := eventctx.NewContext(payload, nil)

	// 使用 Engine 处理事件
	b.engine.ProcessEvent(ctx)
}

// Stop 优雅关闭 Bot
func (b *Bot) Stop(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		logrus.Warn("[Bot] Not running")
		return nil
	}
	b.running = false
	b.stopTime = time.Now()
	b.mu.Unlock()

	logrus.Info("[Bot] Shutting down...")

	// 使用生命周期管理器停止所有组件（逆序）
	if err := b.lifecycle.Stop(ctx); err != nil {
		logrus.WithError(err).Error("[Bot] Stop completed with errors")
		return err
	}

	logrus.Info("[Bot] Stop complete")
	return nil
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
func (b *Bot) Health() *HealthStatus {
	return b.health.Check()
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
