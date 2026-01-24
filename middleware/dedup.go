package middleware

import (
	"fmt"
	"sync"
	"time"

	appconfig "github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/sirupsen/logrus"
)

// DedupFilter 事件去重过滤器
//
// 使用内存缓存实现事件去重，防止重复处理相同事件。
// 适用于防止重放攻击和重复消息处理。
type DedupFilter struct {
	mu          sync.RWMutex
	cache       map[string]int64 // eventID -> expireTime
	maxSize     int              // 最大缓存条目数
	defaultTTL  time.Duration    // 默认过期时间
	cleanupDone chan struct{}    // 清理器停止信号
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
}

// DefaultDedupConfig 返回默认配置
func DefaultDedupConfig() DedupConfig {
	return DedupConfig{
		MaxSize:         10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

// NewDedupFilter 创建新的去重过滤器
func NewDedupFilter(config DedupConfig) *DedupFilter {
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
		cache:       make(map[string]int64),
		maxSize:     config.MaxSize,
		defaultTTL:  config.DefaultTTL,
		cleanupDone: make(chan struct{}),
	}

	// 启动后台清理 goroutine
	go filter.cleanup(config.CleanupInterval)

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
			logrus.WithError(err).Warn("[Dedup] Invalid dedup_default_ttl config, using default 5m")
		}
	}

	cleanupInterval := 1 * time.Minute
	if cfg.DedupCleanupInterval != "" {
		if d, err := time.ParseDuration(cfg.DedupCleanupInterval); err == nil {
			cleanupInterval = d
		} else {
			logrus.WithError(err).Warn("[Dedup] Invalid dedup_cleanup_interval config, using default 1m")
		}
	}

	logrus.Infof("[Dedup] Config: max_size=%d, default_ttl=%v, cleanup_interval=%v",
		maxSize, defaultTTL, cleanupInterval)

	return NewDedupFilter(DedupConfig{
		MaxSize:         maxSize,
		DefaultTTL:      defaultTTL,
		CleanupInterval: cleanupInterval,
	})
}

// CheckDuplicate 检查事件是否重复
//
// 返回 true 表示事件已经存在（重复），false 表示首次出现。
// 如果缓存已满且事件不存在，会先尝试清理过期条目，清理后仍满则返回错误。
func (d *DedupFilter) CheckDuplicate(eventID string) (bool, error) {
	now := time.Now().Unix()

	d.mu.RLock()
	expireTime, exists := d.cache[eventID]
	cacheSize := len(d.cache)
	d.mu.RUnlock()

	// 检查是否存在且未过期
	if exists {
		if expireTime > now {
			return true, nil // 重复且未过期
		}
		// 已过期，删除
		d.mu.Lock()
		delete(d.cache, eventID)
		d.mu.Unlock()
	}

	// 检查缓存大小限制
	if !exists && cacheSize >= d.maxSize {
		// 缓存满载，尝试立即清理过期条目
		logrus.WithFields(logrus.Fields{
			"cache_size": cacheSize,
			"max_size":   d.maxSize,
		}).Debug("[Dedup] Cache full, triggering immediate cleanup")

		d.cleanExpired()

		// 重新检查大小
		d.mu.RLock()
		cacheSize = len(d.cache)
		d.mu.RUnlock()

		if cacheSize >= d.maxSize {
			logrus.WithFields(logrus.Fields{
				"cache_size": cacheSize,
				"max_size":   d.maxSize,
			}).Warn("[Dedup] Cache still full after cleanup")
			return false, fmt.Errorf("dedup cache full after cleanup (size: %d, max: %d)", cacheSize, d.maxSize)
		}

		logrus.WithField("cache_size", cacheSize).Debug("[Dedup] Cache cleaned, space available")
	}

	// 添加到缓存
	d.mu.Lock()
	d.cache[eventID] = now + int64(d.defaultTTL.Seconds())
	d.mu.Unlock()

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
			return
		}
	}
}

// cleanExpired 清理过期条目
func (d *DedupFilter) cleanExpired() {
	now := time.Now().Unix()
	toDelete := make([]string, 0)

	// 收集过期的 eventID
	d.mu.RLock()
	for eventID, expireTime := range d.cache {
		if expireTime <= now {
			toDelete = append(toDelete, eventID)
		}
	}
	d.mu.RUnlock()

	// 删除过期条目
	if len(toDelete) > 0 {
		d.mu.Lock()
		for _, eventID := range toDelete {
			delete(d.cache, eventID)
		}
		d.mu.Unlock()

		logrus.Debugf("[Dedup] Cleaned %d expired entries", len(toDelete))
	}
}

// Stop 停止清理器
func (d *DedupFilter) Stop() {
	close(d.cleanupDone)
}

// GetStats 获取统计信息
func (d *DedupFilter) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"cache_size": len(d.cache),
		"max_size":   d.maxSize,
		"ttl":        d.defaultTTL.String(),
	}
}

// Clear 清空缓存（用于测试）
func (d *DedupFilter) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = make(map[string]int64)
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
func Dedup(filter *DedupFilter) context.Middleware {
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
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
				// 缓存满了，记录警告但继续处理
				logrus.WithError(err).WithField("event_id", eventID).
					Warn("[Dedup] Cache full, processing event anyway")
				return next(ctx)
			}

			if isDup {
				// 重复事件，阻断处理
				logrus.WithField("event_id", eventID).
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
func DedupWithReject(filter *DedupFilter) context.Middleware {
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
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
				logrus.WithError(err).WithField("event_id", eventID).
					Error("[Dedup] Cache full, rejecting event")
				return err
			}

			if isDup {
				logrus.WithField("event_id", eventID).
					Debug("[Dedup] Duplicate event blocked")
				return nil
			}

			return next(ctx)
		}
	}
}
