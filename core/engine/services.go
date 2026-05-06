package engine

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	infrapool "github.com/KomeiDiSanXian/remilia/infra/pool"
)

// engineInternals 集中管理 Engine 的非核心基础设施状态：
//   - 运行时组件（后台 goroutine 管理）
//   - 基础设施关注点（临时 Matcher 管理器、对象池、Metrics 收集器等）
//
// 合并原 services 和 runtime 两个结构体，减少 Engine 顶层字段数量。
type engineInternals struct {
	// runtime 组件管理（后台 goroutine 生命周期）
	runtimeMu    sync.Mutex
	runtimeComps []runtimeComponent

	// metricsCollector 是可选 Prometheus 采集器的原子指针
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
	pendingDeleteDone            chan struct{}
	pendingDeleteProcessInterval time.Duration
	pendingDeleteBatchSize       int

	// commandRegistry 是可选的 command.Registry
	commandRegistry *command.Registry

	// execPoolCfg 是 ExecPool 的配置，在 NewEngine 中由选项设置后创建 pool
	execPoolCfg ExecPoolConfig
	// execPool 是自适应 ExecPool，用于执行被判定为"慢"的 handler。
	// 由 NewEngine 在选项应用后初始化，Shutdown 时 Drain。
	execPool *ExecPool
}

func (e *engineInternals) register(c runtimeComponent) {
	if c == nil {
		return
	}
	if slices.Contains(e.runtimeComps, c) {
		return
	}
	e.runtimeComps = append(e.runtimeComps, c)
}

func (e *engineInternals) stopAll() {
	for _, c := range e.runtimeComps {
		c.stop()
	}
}

func (e *engineInternals) waitAll(ctx context.Context) error {
	for _, c := range e.runtimeComps {
		if err := c.wait(ctx); err != nil {
			return err
		}
	}
	return nil
}
