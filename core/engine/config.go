package engine

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

const (
	// DefaultTempMatcherCleanerInterval 默认临时 Matcher 清理间隔
	DefaultTempMatcherCleanerInterval = 5 * time.Minute
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

// DeadLetterConsumer 接口定义了死信消费器的行为
type DeadLetterConsumer interface {
	Consume(item DeadLetterItem)
}

// WithCleanupInterval 设置临时 Matcher 清理间隔
func WithCleanupInterval(interval time.Duration) Option {
	return func(e *Engine) {
		e.s.tempMatcherCleanerInterval = interval
	}
}

// WithPendingDeleteBufferSize 设置批量删除通道的大小
func WithPendingDeleteBufferSize(size int) Option {
	return func(e *Engine) {
		e.s.pendingDeleteCh = make(chan *Matcher, size)
	}
}
