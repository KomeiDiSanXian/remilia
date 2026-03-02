package engine

import (
	"time"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	infrapool "github.com/KomeiDiSanXian/remilia/infra/pool"
)

// engineServices groups non-core Engine concerns:
// - temp matcher lifecycle store
// - pending delete queue
// - matcher slice pool
// - metrics collector holder
//
// The goal is to keep Engine'services core routing/matching state separate from
// runtime/infra concerns while keeping the external Engine API stable.
//
// NOTE: this struct is internal and may change at any time.
type engineServices struct {
	// metricsCollector is a type-safe atomic pointer to the optional Prometheus
	// metrics collector. nil means metrics are disabled.
	metricsCollector *infraatomic.Value[*metrics.Collector]

	// temp matcher lifecycle store
	tempManager *tempMatcherManager

	// matcher slice pool
	matcherPool *infrapool.TypedPool[[]*Matcher]

	// temp cleaner config/state
	tempMatcherCleanerStop     func()
	tempMatcherCleanerInterval time.Duration
	tempMatcherCleanerDone     chan struct{}

	// pending delete config/state
	pendingDeleteCh              chan *Matcher
	pendingDeleteStop            func()
	pendingDeleteProcessInterval time.Duration // 批量删除处理间隔
	pendingDeleteBatchSize       int           // 每次批量删除数量
}
