package engine

import (
	"sync/atomic"
	"time"

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
	metricsCollector atomic.Value // *MetricsCollector

	// temp matcher lifecycle store
	tempManager *tempMatcherManager

	// matcher slice pool
	matcherPool *infrapool.TypedPool[[]*Matcher]

	// temp cleaner config/state
	tempMatcherCleanerStop     func()
	tempMatcherCleanerInterval time.Duration
	tempMatcherCleanerDone     chan struct{}

	// pending delete
	pendingDeleteCh   chan *Matcher
	pendingDeleteStop func()
}
