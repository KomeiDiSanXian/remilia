package engine

import (
	"sync"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// EngineManager 管理 per-channel Engine 实例。
// 每个 channel 拥有独立的 Engine，实现 matcher 隔离和 Block() 互不影响。
//
// 使用方式：
//
//	em := engine.NewEngineManager(templateEngine)
//	// bot.go 中优先走 em.Dispatch(ctx, event)
//	// em 内部自动创建/复用 channel engine，懒同步模板变化
type EngineManager struct {
	template  *Engine
	instances sync.Map
	maxIdle   time.Duration

	stopGC   chan struct{}
	once     sync.Once
	createMu sync.Mutex // 保护并发首次创建，避免 newChannelEngine 重复执行
}

// ManagerOption 配置 EngineManager 的选项。
type ManagerOption func(*EngineManager)

// WithMaxIdle 设置 channel engine 空闲淘汰时间。默认 30 分钟。
func WithMaxIdle(d time.Duration) ManagerOption {
	return func(em *EngineManager) {
		em.maxIdle = d
	}
}

// NewEngineManager 创建一个 EngineManager，使用 template 作为全局模板引擎。
// 默认空闲淘汰时间为 30 分钟。
func NewEngineManager(template *Engine, opts ...ManagerOption) *EngineManager {
	em := &EngineManager{
		template: template,
		maxIdle:  30 * time.Minute,
		stopGC:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(em)
	}
	return em
}

// Dispatch 将事件分发到对应 channel 的 Engine。
// 如果 channel 的 Engine 不存在，自动从模板创建。
// 首次调用时自动启动后台 GC goroutine，无需手动调用
func (em *EngineManager) Dispatch(ctx *corectx.Context) {
	if ctx == nil {
		return
	}

	// 首次 Dispatch 自动启动后台 GC，防止调用方忘记 StartGC
	em.once.Do(func() {
		go em.gcLoop()
	})

	channelKey := MakeChannelKey(ctx.GetEventPlatform(), ctx.GetChatInfo().ID)

	// 快速路径：channel 已存在
	if actual, ok := em.instances.Load(channelKey); ok {
		actual.(*Engine).ProcessEvent(ctx)
		return
	}

	// 慢速路径：加锁创建，避免并发首次访问时 newChannelEngine 执行多次
	em.createMu.Lock()
	if actual, ok := em.instances.Load(channelKey); ok {
		em.createMu.Unlock()
		actual.(*Engine).ProcessEvent(ctx)
		return
	}
	chEngine := em.newChannelEngine(channelKey)
	em.instances.Store(channelKey, chEngine)
	em.createMu.Unlock()
	chEngine.ProcessEvent(ctx)
}

// newChannelEngine 从模板创建一个新的 channel Engine。
func (em *EngineManager) newChannelEngine(key ChannelKey) *Engine {
	child := NewEngine()
	child.ForkFrom(em.template, key)

	logger.WithFields(logger.Fields{
		"channel": string(key),
	}).Debug("[engine] Created per-channel engine")

	return child
}

// gcLoop 定时扫描并清理空闲 channel Engine。
// 通过 em.stopGC 通道停止（由 Close 关闭）。
func (em *EngineManager) gcLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			em.evictIdle()
		case <-em.stopGC:
			return
		}
	}
}

// evictIdle 清理所有超过 maxIdle 未使用的 channel Engine。
func (em *EngineManager) evictIdle() {
	now := time.Now().Unix()
	threshold := now - int64(em.maxIdle.Seconds())

	em.instances.Range(func(key, value any) bool {
		chEngine := value.(*Engine)
		if chEngine.LastUsed() < threshold {
			em.instances.Delete(key)
			logger.WithFields(logger.Fields{
				"channel": string(key.(ChannelKey)),
			}).Debug("[engine] Evicted idle per-channel engine")
		}
		return true
	})
}

// GetChannel 返回指定 channel 的 Engine，不存在时返回 nil。
func (em *EngineManager) GetChannel(key ChannelKey) *Engine {
	if v, ok := em.instances.Load(key); ok {
		return v.(*Engine)
	}
	return nil
}

// Stats 返回 EngineManager 的统计信息。
func (em *EngineManager) Stats() map[string]any {
	count := 0
	em.instances.Range(func(_, _ any) bool {
		count++
		return true
	})
	return map[string]any{
		"channel_count": count,
	}
}

// Close 关闭 EngineManager，停止 GC。
func (em *EngineManager) Close() {
	close(em.stopGC)
}
