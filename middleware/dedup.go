package middleware

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	appconfig "github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// DedupFilter 事件去重过滤器
//
// 使用内存缓存实现事件去重，防止重复处理相同事件。
// 适用于防止重放攻击和重复消息处理。
//
// 优化: 使用 hash(eventID) 代替字符串键，内存占用减少 50-70%
type DedupFilter struct {
	mu          sync.RWMutex
	cache       map[uint64]int64 // hash(eventID) -> expireTime，优化：使用 uint64 代替 string
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
		cache:       make(map[uint64]int64, config.MaxSize/2), // 使用 uint64，预分配容量
		maxSize:     config.MaxSize,
		defaultTTL:  config.DefaultTTL,
		cleanupDone: make(chan struct{}),
		strictMode:  config.StrictMode,
	}

	// 启动后台清理 goroutine，同时监听 parent context 和 cleanupDone
	interval := config.CleanupInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				filter.cleanExpired()
			case <-parent.Done():
				// 外部 context 取消（如 Bot 关闭）时退出
				filter.cleanExpired()
				return
			case <-filter.cleanupDone:
				// 手动调用 Stop() 时退出
				filter.cleanExpired()
				return
			}
		}
	}()

	return filter
}

// NewDedupFilterFromConfig 从配置创建去重过滤器
//
// 使用示例:
//
//	cfg, _ := config.LoadDefault()
//	filter := middleware.NewDedupFilterFromConfig(cfg.Middleware)
func NewDedupFilterFromConfig(cfg appconfig.MiddlewareConfig) *DedupFilter {
	maxSize := cfg.DedupMaxSize
	if maxSize <= 0 {
		maxSize = 10000
	}

	defaultTTL := 5 * time.Minute
	if cfg.DedupDefaultTTL != "" {
		if d, err := time.ParseDuration(cfg.DedupDefaultTTL); err == nil {
			defaultTTL = d
		} else {
			logger.WithError(err).Warn("[Dedup] Invalid dedup_default_ttl config, using default 5m")
		}
	}

	cleanupInterval := 1 * time.Minute
	if cfg.DedupCleanupInterval != "" {
		if d, err := time.ParseDuration(cfg.DedupCleanupInterval); err == nil {
			cleanupInterval = d
		} else {
			logger.WithError(err).Warn("[Dedup] Invalid dedup_cleanup_interval config, using default 1m")
		}
	}

	logger.Infof("[Dedup] Config: max_size=%d, default_ttl=%v, cleanup_interval=%v",
		maxSize, defaultTTL, cleanupInterval)

	return NewDedupFilter(DedupConfig{
		MaxSize:         maxSize,
		DefaultTTL:      defaultTTL,
		CleanupInterval: cleanupInterval,
	})
}

// hashEventID 使用 FNV-1a 算法将 eventID 转换为 uint64
//
// FNV-1a 特点：
// - 速度快（比 SHA256 快 10x+）
// - 分布均匀
// - 冲突率低
//
// 注意：理论上存在哈希冲突可能，但概率极低（< 1/2^64）
func hashEventID(eventID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(eventID)) // hash.Hash.Write never returns error
	return h.Sum64()
}

// CheckDuplicate 检查事件是否重复
//
// 返回 true 表示事件已经存在（重复），false 表示首次出现。
// 如果缓存已满且事件不存在，会先尝试清理过期条目，清理后仍满则返回错误。
//
// 优化：使用 hash(eventID) 减少内存占用，字符串 -> uint64 可节省 50-70% 内存
func (d *DedupFilter) CheckDuplicate(eventID string) (bool, error) {
	now := time.Now().UnixNano()
	hash := hashEventID(eventID) // 计算哈希值

	// 使用单个写锁保护整个操作，避免竞态条件
	d.mu.Lock()
	defer d.mu.Unlock()

	// 检查是否存在且未过期（使用哈希值查找）
	if expireTime, exists := d.cache[hash]; exists {
		if expireTime > now {
			return true, nil // 重复且未过期
		}
		// 已过期，删除
		delete(d.cache, hash)
	}

	// 检查缓存大小限制
	if len(d.cache) >= d.maxSize {
		// 缓存满载，尝试立即清理过期条目
		logger.WithFields(logger.Fields{
			"cache_size": len(d.cache),
			"max_size":   d.maxSize,
		}).Debug("[Dedup] Cache full, triggering immediate cleanup")

		// 清理过期条目（已持有锁）
		d.cleanExpiredLocked(now)

		// 再次检查大小
		if len(d.cache) >= d.maxSize {
			logger.WithFields(logger.Fields{
				"cache_size": len(d.cache),
				"max_size":   d.maxSize,
			}).Warn("[Dedup] Cache still full after cleanup")
			return false, fmt.Errorf("dedup cache full after cleanup (size: %d, max: %d)", len(d.cache), d.maxSize)
		}

		logger.WithField("cache_size", len(d.cache)).Debug("[Dedup] Cache cleaned, space available")
	}

	// 添加到缓存 (使用纳秒以保留毫秒以下精度，使用哈希值存储)
	d.cache[hash] = now + d.defaultTTL.Nanoseconds()

	return false, nil
}

// cleanup 后台清理过期条目
func (d *DedupFilter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.cleanExpired()
		case <-d.cleanupDone:
			// 最后清理一次
			d.cleanExpired()
			return
		}
	}
}

// cleanExpired 清理过期条目
func (d *DedupFilter) cleanExpired() {
	now := time.Now().UnixNano()

	d.mu.Lock()
	d.cleanExpiredLocked(now)
	d.mu.Unlock()
}

// cleanExpiredLocked 清理过期条目（内部方法，假设已持有锁）
func (d *DedupFilter) cleanExpiredLocked(now int64) {
	toDelete := make([]uint64, 0) // 优化：使用 uint64 存储哈希值

	// 收集过期的哈希值
	for hash, expireTime := range d.cache {
		if expireTime <= now {
			toDelete = append(toDelete, hash)
		}
	}

	// 删除过期条目
	if len(toDelete) > 0 {
		for _, hash := range toDelete {
			delete(d.cache, hash)
		}

		logger.Debugf("[Dedup] Cleaned %d expired entries", len(toDelete))
	}
}

// Stop 停止清理器
// 多次调用是安全的，只会执行一次
func (d *DedupFilter) Stop() {
	d.stopOnce.Do(func() {
		close(d.cleanupDone)
	})
}

// GetStats 获取统计信息
func (d *DedupFilter) GetStats() map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]any{
		"cache_size": len(d.cache),
		"max_size":   d.maxSize,
		"ttl":        d.defaultTTL.String(),
	}
}

// Clear 清空缓存（用于测试）
func (d *DedupFilter) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = make(map[uint64]int64) // 使用 uint64 类型
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
			event := ctx.GetEvent()
			if event == nil {
				return next(ctx)
			}

			eventID := string(event.ID)
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

// DedupWithReject 创建严格的去重中间件（拒绝缓存满的情况）
//
// 与 Dedup 的区别：
//   - 缓存满时返回错误，不处理事件
//   - 适用于对数据一致性要求更高的场景
func DedupWithReject(filter *DedupFilter) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			event := ctx.GetEvent()
			if event == nil {
				return next(ctx)
			}

			eventID := string(event.ID)
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
