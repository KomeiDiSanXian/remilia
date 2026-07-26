package engine

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
)

// engineInternals 集中管理 Engine 的非核心基础设施状态：
//   - 运行时组件（后台 goroutine 管理）
//   - 基础设施关注点（临时 Matcher 管理器、对象池、Metrics 收集器等）
//
// 合并原 services 和 runtime 两个结构体，减少 Engine 顶层字段数量。
type engineInternals struct {
	// runtime 组件管理（后台 goroutine 生命周期）
	runtimeComps []runtimeComponent

	// metricsCollector 是可选 Prometheus 采集器的原子指针
	metricsCollector *infraatomic.Value[*metrics.Collector]

	// temp matcher lifecycle store
	tempManager *tempMatcherManager

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
	// pendingDeleteActive 标记批量删除处理器是否在运行。
	// DeleteMatcher 据此决定入队（批量路径）还是同步 COW 删除（回退路径）；
	// 用 atomic 以便在热路径无锁读取。
	pendingDeleteActive atomic.Bool

	// commandRegistry 是可选的 command.Registry
	commandRegistry *command.Registry

	// execPoolCfg 是 ExecPool 的配置，在 NewEngine 中由选项设置后创建 pool
	execPoolCfg ExecPoolConfig
	// execPool 是自适应 ExecPool，用于执行被判定为"慢"的 handler。
	// 未通过 WithSharedExecPool 注入时由 NewEngine 在选项应用后创建，Shutdown 时 Drain。
	execPool *ExecPool
	// execPoolShared 为 true 表示 execPool 由 WithSharedExecPool 注入、
	// 生命周期归调用方所有：Engine.Shutdown 不会 Drain/停止它。
	execPoolShared bool
	// dispatcherCfg 是 OutboundDispatcher 的配置
	dispatcherCfg DispatcherConfig
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
