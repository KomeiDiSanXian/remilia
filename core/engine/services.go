package engine

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	infrapool "github.com/KomeiDiSanXian/remilia/infra/pool"
)

// services groups non-core Engine concerns.
type services struct {
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
	pendingDeleteProcessInterval time.Duration
	pendingDeleteBatchSize       int

	// commandRegistry 是可选的 command.Registry，若已注入则 OnCommand/RegisterCommand
	// 在更新 engine 内部 commandIndex 的同时自动同步注册到此 Registry，
	// 消除双轨并行维护，使 Trie 前缀搜索与 /help 发现统一生效。
	// nil 表示未使用 command.Registry（纯 commandIndex 模式，向后兼容）。
	commandRegistry *command.Registry
}
