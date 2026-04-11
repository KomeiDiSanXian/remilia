package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// DedupFilter 事件去重过滤器
//
// 使用内存缓存实现事件去重，防止重复处理相同事件。
// 适用于防止重放攻击和重复消息处理。
//
// # 实现说明
//
// 内部使用 map[string]int64（eventID 字符串 → 过期时间戳纳秒）作为缓存。
// 对于所有 eventID 唯一性要求严格的场景（包括高吞吐机器人）均可放心使用。
//
// # 性能注意事项
//
// 当 MaxSize > 10000 且 DefaultTTL 超过 1 分钟时，map[string]int64 中字符串键
// 会被 Go GC 逐一扫描，GC 压力随条目数线性增长。高容量长 TTL 场景可考虑
// 使用 bigcache/freecache 等减少 GC 压力的方案
type DedupFilter struct {
	mu          sync.RWMutex
	cache       map[string]int64 // eventID -> expireTime（纳秒）
	maxSize     int              // 最大缓存条目数
	defaultTTL  time.Duration    // 默认过期时间
	cleanupDone chan struct{}    // 清理器停止信号
	strictMode  bool             // 严格模式：cache满时是否拒绝事件
	stopOnce    sync.Once        // 确保Stop只执行一次
}

// DedupConfig 去重配置
type DedupConfig struct {
	// MaxSize 最大缓存条目数，超过后会拒绝新事件
	// 默认: 10000
	MaxSize int

	// DefaultTTL 默认过期时间，过期后自动删除
	// 默认: 5 分钟
	DefaultTTL time.Duration

	// CleanupInterval 清理过期条目的间隔
	// 默认: 1 分钟
	CleanupInterval time.Duration

	// StrictMode 严格模式：cache 满时拒绝处理事件而不是允许通过
	// true: 拒绝事件，返回错误
	// false: 允许事件通过（可能重复）
	// 默认: false
	StrictMode bool
}

// DefaultDedupConfig 返回默认配置
func DefaultDedupConfig() DedupConfig {
	return DedupConfig{
		MaxSize:         10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

// NewDedupFilter 创建新的去重过滤器（使用 context.Background() 作为根 context）
//
// 注意：调用方应在不再使用时调用 filter.Stop() 释放后台 goroutine。
// 若想让 goroutine 与外部生命周期自动联动，请使用 NewDedupFilterWithContext。
func NewDedupFilter(config DedupConfig) *DedupFilter {
	return NewDedupFilterWithContext(context.Background(), config)
}

// NewDedupFilterWithContext 创建与外部 context 联动的去重过滤器。
//
// 当 parent ctx 被取消时（如 Bot 关闭），后台 cleanup goroutine 自动退出，
// 无需手动调用 Stop()。与 AdaptiveRateLimiter、token.Manager 的 WithContext 模式一致。
//
// 推荐在 Bot 生命周期中使用：
//
//	filter := middleware.NewDedupFilterWithContext(bot.Context(), config)
func NewDedupFilterWithContext(parent context.Context, config DedupConfig) *DedupFilter {
	if config.MaxSize <= 0 {
		config.MaxSize = 10000
	}
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	filter := &DedupFilter{
		cache:       make(map[string]int64, config.MaxSize/2),
		maxSize:     config.MaxSize,
		defaultTTL:  config.DefaultTTL,
		cleanupDone: make(chan struct{}),
		strictMode:  config.StrictMode,
	}

	// 启动后台清理 goroutine：通过调用 cleanup() 统一逻辑，避免与该方法的实现分叉。
	// 同时监听 parent context（Bot 关闭）和 cleanupDone（手动 Stop()）。
	interval := config.CleanupInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				filter.cleanExpired()
			case <-parent.Done():
				filter.cleanExpired()
				return
			case <-filter.cleanupDone:
				filter.cleanExpired()
				return
			}
		}
	}()

	return filter
}

// CheckDuplicate 检查事件是否重复
//
// 返回 true 表示事件已经存在（重复），false 表示首次出现。
// 如果缓存已满且事件不存在，会先尝试清理过期条目，清理后仍满则返回错误。
func (f *DedupFilter) CheckDuplicate(eventID string) (bool, error) {
	now := time.Now().UnixNano()

	// 使用单个写锁保护整个操作，避免竞态条件
	f.mu.Lock()
	defer f.mu.Unlock()

	// 检查是否存在且未过期
	if expireTime, exists := f.cache[eventID]; exists {
		if expireTime > now {
			return true, nil // 重复且未过期
		}
		// 已过期，删除
		delete(f.cache, eventID)
	}

	// 检查缓存大小限制
	if len(f.cache) >= f.maxSize {
		// 缓存满载，尝试立即清理过期条目
		logger.WithFields(logger.Fields{
			"cache_size": len(f.cache),
			"max_size":   f.maxSize,
		}).Debug("[Dedup] Cache full, triggering immediate cleanup")

		// 清理过期条目（已持有锁）
		f.cleanExpiredLocked(now)

		// 再次检查大小
		if len(f.cache) >= f.maxSize {
			logger.WithFields(logger.Fields{
				"cache_size": len(f.cache),
				"max_size":   f.maxSize,
			}).Warn("[Dedup] Cache still full after cleanup")
			return false, fmt.Errorf("dedup cache full after cleanup (size: %d, max: %d): %w", len(f.cache), f.maxSize, errutil.ErrDedupCacheFull)
		}

		logger.WithField("cache_size", len(f.cache)).Debug("[Dedup] Cache cleaned, space available")
	}

	// 添加到缓存（使用纳秒以保留毫秒以下精度）
	f.cache[eventID] = now + f.defaultTTL.Nanoseconds()

	return false, nil
}

// cleanExpired 清理过期条目
func (f *DedupFilter) cleanExpired() {
	now := time.Now().UnixNano()

	f.mu.Lock()
	f.cleanExpiredLocked(now)
	f.mu.Unlock()
}

// cleanExpiredLocked 清理过期条目（内部方法，假设已持有锁）
func (f *DedupFilter) cleanExpiredLocked(now int64) {
	toDelete := make([]string, 0)

	// 收集过期的 eventID
	for eventID, expireTime := range f.cache {
		if expireTime <= now {
			toDelete = append(toDelete, eventID)
		}
	}

	// 删除过期条目
	if len(toDelete) > 0 {
		for _, eventID := range toDelete {
			delete(f.cache, eventID)
		}

		logger.Debugf("[Dedup] Cleaned %d expired entries", len(toDelete))
	}
}

// Stop 停止清理器
// 多次调用是安全的，只会执行一次
func (f *DedupFilter) Stop() {
	f.stopOnce.Do(func() {
		close(f.cleanupDone)
	})
}

// GetStats 获取统计信息
func (f *DedupFilter) GetStats() map[string]any {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return map[string]any{
		"cache_size": len(f.cache),
		"max_size":   f.maxSize,
		"ttl":        f.defaultTTL.String(),
	}
}

// Clear 清空缓存（用于测试）
func (f *DedupFilter) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache = make(map[string]int64)
}

// Dedup 创建去重中间件
//
// 使用示例：
//
//	filter := middleware.NewDedupFilter(middleware.DefaultDedupConfig())
//	defer filter.Stop()
//
//	engine.Use(middleware.Dedup(filter))
//
// 注意：
//   - 重复事件会被阻断，不会调用 handler
//   - 缓存满时会返回错误并继续处理（避免拒绝服务）
//   - 需要手动调用 filter.Stop() 停止后台清理
func Dedup(filter *DedupFilter) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			pe := ctx.GetPlatformEvent()
			if pe == nil {
				return next(ctx)
			}

			eventID := pe.ID()
			if eventID == "" {
				// 没有 eventID，跳过去重检查
				return next(ctx)
			}

			isDup, err := filter.CheckDuplicate(eventID)
			if err != nil {
				// 缓存满了
				if filter.strictMode {
					// 严格模式：拒绝事件
					logger.WithError(err).WithField("event_id", eventID).
						Error("[Dedup] Cache full in strict mode, rejecting event")
					return err
				}
				// 非严格模式：记录警告但继续处理
				logger.WithError(err).WithField("event_id", eventID).
					Warn("[Dedup] Cache full, processing event anyway (best-effort mode)")
				return next(ctx)
			}

			if isDup {
				// 重复事件，阻断处理
				logger.WithField("event_id", eventID).
					Debug("[Dedup] Duplicate event blocked")
				return nil // 不返回错误，只是跳过处理
			}

			return next(ctx)
		}
	}
}

// UpdateConfig 热更新去重过滤器配置（线程安全，立即生效）。
//
// 仅更新 MaxSize、DefaultTTL 两项运行时可变参数。
// CleanupInterval 变更需要重建过滤器（修改 ticker 代价较高）。
func (f *DedupFilter) UpdateConfig(cfg DedupConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cfg.MaxSize > 0 {
		f.maxSize = cfg.MaxSize
	}
	if cfg.DefaultTTL > 0 {
		f.defaultTTL = cfg.DefaultTTL
	}
}

// DedupWithReject 创建严格的去重中间件（拒绝缓存满的情况）
//
// 与 Dedup 的区别：
//   - 缓存满时返回错误，不处理事件
//   - 适用于对数据一致性要求更高的场景
func DedupWithReject(filter *DedupFilter) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			pe := ctx.GetPlatformEvent()
			if pe == nil {
				return next(ctx)
			}

			eventID := pe.ID()
			if eventID == "" {
				return next(ctx)
			}

			isDup, err := filter.CheckDuplicate(eventID)
			if err != nil {
				// 缓存满了，返回错误
				logger.WithError(err).WithField("event_id", eventID).
					Error("[Dedup] Cache full, rejecting event")
				return err
			}

			if isDup {
				logger.WithField("event_id", eventID).
					Debug("[Dedup] Duplicate event blocked")
				return nil
			}

			return next(ctx)
		}
	}
}
