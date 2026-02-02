package engine

import (
	"time"

	appconfig "github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrapool "github.com/KomeiDiSanXian/remilia/infra/pool"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

const (
	// DefaultTempMatcherCleanerInterval 默认临时 Matcher 清理间隔
	DefaultTempMatcherCleanerInterval = 1 * time.Minute
	// DefaultPendingDeleteBufferSize 默认批量删除通道大小
	DefaultPendingDeleteBufferSize = 1000
	// DefaultMatcherPoolCapacity 默认 Matcher 池初始容量
	DefaultMatcherPoolCapacity = 16
	// MaxMatcherPoolRetainCapacity Matcher 池回收的最大容量，防止无限增长
	MaxMatcherPoolRetainCapacity = 1024
	// DefaultPendingDeleteProcessInterval 默认批量删除处理间隔
	DefaultPendingDeleteProcessInterval = 100 * time.Millisecond
	// DefaultPendingDeleteBatchSize 默认每次批量删除数量
	DefaultPendingDeleteBatchSize = 1000
)

// DeadLetterItem 代表死信队列中的一项
type DeadLetterItem struct {
	Event   *dto.Payload
	Err     error
	Attempt int
	Source  string
}

// WithCleanupInterval 设置临时 Matcher 清理间隔
func WithCleanupInterval(interval time.Duration) Option {
	return func(e *Engine) {
		e.services.tempMatcherCleanerInterval = interval
	}
}

// WithPendingDeleteBufferSize 设置批量删除通道的大小
func WithPendingDeleteBufferSize(size int) Option {
	return func(e *Engine) {
		e.services.pendingDeleteCh = make(chan *Matcher, size)
	}
}

// WithConfig 从配置创建 Engine 的所有选项
//
// 参数:
//   - cfg: Engine 配置（从 config.Config.Engine 获取）
//
// 示例:
//
//	cfg, _ := config.LoadDefault()
//	engine := engine.NewEngine(engine.WithConfig(cfg.Engine))
func WithConfig(cfg appconfig.EngineConfig) Option {
	return func(e *Engine) {
		// 解析并应用临时 Matcher 清理间隔
		if cfg.TempMatcherCleanupInterval != "" {
			if interval, err := time.ParseDuration(cfg.TempMatcherCleanupInterval); err == nil {
				e.services.tempMatcherCleanerInterval = interval
			} else {
				logger.WithError(err).Warnf("[Engine] Invalid temp_matcher_cleanup_interval config, using default %v",
					DefaultTempMatcherCleanerInterval)
			}
		}

		// 应用批量删除缓冲区大小
		if cfg.PendingDeleteBufferSize > 0 {
			e.services.pendingDeleteCh = make(chan *Matcher, cfg.PendingDeleteBufferSize)
		}

		// 应用批量删除处理间隔
		if cfg.PendingDeleteProcessInterval != "" {
			if interval, err := time.ParseDuration(cfg.PendingDeleteProcessInterval); err == nil {
				e.services.pendingDeleteProcessInterval = interval
			} else {
				logger.WithError(err).Warnf("[Engine] Invalid pending_delete_process_interval config, using default %v",
					DefaultPendingDeleteProcessInterval)
			}
		}

		// 应用批量删除批次大小
		if cfg.PendingDeleteBatchSize > 0 {
			e.services.pendingDeleteBatchSize = cfg.PendingDeleteBatchSize
		}

		// 应用 Matcher 池容量配置
		if cfg.MatcherPoolCapacity > 0 {
			e.services.matcherPool = infrapool.New(func() []*Matcher {
				return make([]*Matcher, 0, cfg.MatcherPoolCapacity)
			})
		}

		// 注意：MaxMatcherPoolCapacity 和 TempMatcherShardCount 需要在池创建时使用
		// 这些配置可以在后续优化中支持

		logger.Infof("[Engine] Config applied: cleanup_interval=%v, delete_buffer=%d, process_interval=%v, batch_size=%d, pool_capacity=%d",
			e.services.tempMatcherCleanerInterval,
			cap(e.services.pendingDeleteCh),
			e.services.pendingDeleteProcessInterval,
			e.services.pendingDeleteBatchSize,
			cfg.MatcherPoolCapacity,
		)
	}
}
