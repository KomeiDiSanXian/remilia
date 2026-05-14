package dedup

import (
	"context"
	"fmt"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

const maxEventIDLength = 256

// DedupFilter 事件去重过滤器
//
// 使用内存缓存实现事件去重，防止重复处理相同事件。
// 适用于防止重放攻击和重复消息处理。
//
// # 优化说明
//
// CheckDuplicate 使用 RWMutex 优化：重复（已存在且未过期）事件在 RLock 下检查，
// 避免不必要的写锁竞争。仅新事件（需要写入）时升级为写锁。
//
// 超过 256 字符的 eventID 会被哈希后存储，防止超大 key 导致内存膨胀。
type DedupFilter struct {
	mu          sync.RWMutex
	cache       map[string]int64 // eventID -> expireTime（纳秒）
	maxSize     int              // 最大缓存条目数
	defaultTTL  time.Duration    // 默认过期时间
	cleanupDone chan struct{}    // 清理器停止信号
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

	// CleanupInterval 清理间隔
	// 默认: 1 分钟
	CleanupInterval time.Duration
}

// DefaultDedupConfig 返回默认配置
func DefaultDedupConfig() DedupConfig {
	return DedupConfig{
		MaxSize:         10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

// NewDedupFilter 创建简单的去重过滤器（无后台 goroutine）。
//
// 与 NewDedupFilterWithContext 不同，此函数不会启动后台清理 goroutine，
// 过期条目会在 CheckDuplicate 路径满时被内联清理。
// 如需后台主动清理及生命周期联动，请使用 NewDedupFilterWithContext。
func NewDedupFilter(config DedupConfig) *DedupFilter {
	if config.MaxSize <= 0 {
		config.MaxSize = 10000
	}
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}
	return &DedupFilter{
		cache:       make(map[string]int64, config.MaxSize/2),
		maxSize:     config.MaxSize,
		defaultTTL:  config.DefaultTTL,
		cleanupDone: make(chan struct{}),
	}
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
	}

	// 启动后台清理 goroutine
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

// normalizeEventID 对超长 eventID 进行哈希截断，防止 map key 过大导致内存膨胀
func normalizeEventID(eventID string) string {
	if len(eventID) > maxEventIDLength {
		var h uint64
		for i := 0; i < len(eventID); i++ {
			h = h*131 + uint64(eventID[i])
		}
		return fmt.Sprintf("hash:%x", h)
	}
	return eventID
}

// CheckDuplicate 检查事件是否重复
//
// 返回 true 表示事件已经存在（重复），false 表示首次出现。
// 如果缓存已满且事件不存在，会先尝试清理过期条目，清理后仍满则返回错误。
//
// 优化：重复事件（已存在且未过期）在 RLock 下检查，避免写锁竞争。
func (f *DedupFilter) CheckDuplicate(eventID string) (bool, error) {
	now := time.Now().UnixNano()
	eventID = normalizeEventID(eventID)

	// 快速路径：在 RLock 下检查重复
	f.mu.RLock()
	if expireTime, exists := f.cache[eventID]; exists {
		if expireTime > now {
			f.mu.RUnlock()
			return true, nil // 重复且未过期
		}
	}
	f.mu.RUnlock()

	// 写路径
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check 防止 race（RLock 释放到 Lock 获取之间的窗口）
	if expireTime, exists := f.cache[eventID]; exists {
		if expireTime > now {
			return true, nil
		}
		delete(f.cache, eventID)
	}

	// 检查缓存大小限制
	if len(f.cache) >= f.maxSize {
		logger.WithFields(logger.Fields{
			"cache_size": len(f.cache),
			"max_size":   f.maxSize,
		}).Debug("[Dedup] Cache full, triggering immediate cleanup")

		toDelete := make([]string, 0, len(f.cache)/4)
		for eid, expireTime := range f.cache {
			if expireTime <= now {
				toDelete = append(toDelete, eid)
			}
		}
		for _, eid := range toDelete {
			delete(f.cache, eid)
		}

		if len(f.cache) >= f.maxSize {
			logger.WithFields(logger.Fields{
				"cache_size": len(f.cache),
				"max_size":   f.maxSize,
			}).Warn("[Dedup] Cache still full after cleanup")
			return false, fmt.Errorf("dedup cache full after cleanup (size: %d, max: %d): %w", len(f.cache), f.maxSize, errutil.ErrDedupCacheFull)
		}
	}

	// 添加到缓存
	f.cache[eventID] = now + f.defaultTTL.Nanoseconds()
	return false, nil
}

// cleanExpired 清理过期条目
func (f *DedupFilter) cleanExpired() {
	now := time.Now().UnixNano()

	f.mu.Lock()
	toDelete := make([]string, 0, len(f.cache)/4)
	for eid, expireTime := range f.cache {
		if expireTime <= now {
			toDelete = append(toDelete, eid)
		}
	}
	for _, eid := range toDelete {
		delete(f.cache, eid)
	}
	f.mu.Unlock()
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
	f.cache = make(map[string]int64, f.maxSize/2)
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
				return next(ctx)
			}

			isDup, err := filter.CheckDuplicate(eventID)
			if err != nil {
				// 缓存满了，记录警告但继续处理
				logger.WithError(err).WithField("event_id", eventID).
					Warn("[Dedup] Cache full, processing event anyway (best-effort mode)")
				return next(ctx)
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

// SimpleDedup 创建带默认配置的去重中间件
//
// 使用示例:
//
//	engine.Use(middleware.SimpleDedup())
//
// 注意：此便捷函数创建的过滤器不会启动后台清理 goroutine，
// 过期条目会在 CheckDuplicate 路径满时被内联清理。
// 如需后台主动清理，请使用 NewDedupFilterWithContext + Dedup 组合。
func SimpleDedup() eventctx.Middleware {
	config := DedupConfig{
		MaxSize:    10000,
		DefaultTTL: 5 * time.Minute,
	}
	filter := NewDedupFilter(config)
	return Dedup(filter)
}

// SimpleDedupWithTTL 创建指定TTL的去重中间件
//
// 使用示例:
//
//	engine.Use(middleware.SimpleDedupWithTTL(5 * time.Minute))
//
// 注意：此便捷函数创建的过滤器不会启动后台清理 goroutine。
func SimpleDedupWithTTL(ttl time.Duration) eventctx.Middleware {
	config := DedupConfig{
		MaxSize:    10000,
		DefaultTTL: ttl,
	}
	filter := NewDedupFilter(config)
	return Dedup(filter)
}
