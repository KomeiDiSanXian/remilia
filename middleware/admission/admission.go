// Package admission 按「handler 成本类别」做准入控制。
//
// 与降级的区别：
//   - 降级（middleware/degradation）回答"系统是否过载"，依据 CPU/内存；
//   - 准入（本包）回答"还能不能再接一个"，依据**这条 handler 会占多久槽位**。
//
// 等待外部 API 的 handler（AI 回合、搜索、天气）会长时间占着槽位，但等待本身
// 不代表过载。把它们和 /help 之类廉价请求放进同一个信号量，慢请求会挤掉快请求。
// 本包给两类 handler 独立预算：
//
//   - local：廉价 handler，立即拿槽，满了直接拒绝（这种才是真过载）；
//   - io：外部 I/O 型 handler，允许有界排队，队满才拒绝。
//
// 类别来源完全在框架内部完成，**插件无需为中心表登记**：
//
//  1. 插件/调用方显式声明（[plugin.Descriptor.Cost] / [engine.Matcher.SetCost]）；
//  2. 引擎为每条 matcher 维护的耗时画像（p50 ≥ SlowP50 判为 I/O 型）；
//  3. 冷启动/未知按 local 放行，避免新插件第一条消息就被排队。
//
// 被拒绝的事件返回 [eventctx.ErrBusy]；引擎捕获后调用该 matcher 通过
// [engine.Matcher.OnReject] 声明的忙时处理器，由插件决定文案。
package admission

import (
	"sync/atomic"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	inframetrics "github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// ClassConfig 单个成本类别的预算。
type ClassConfig struct {
	// Limit 并发上限；<= 0 表示该类不限制（直接放行）。
	Limit int
	// Queue 允许排队的等待者数量；<= 0 表示不排队，拿不到槽立即拒绝。
	Queue int
	// WaitTimeout 排队等待上限；0 表示跟随 ctx 的 deadline。
	WaitTimeout time.Duration
}

// Config 准入控制配置。
type Config struct {
	// SlowP50 判定「I/O 型」的耗时阈值（p50 >= 该值），默认 500ms。
	SlowP50 time.Duration
	// MinSamples 冷启动样本阈值：样本不足一律按 local，默认 3。
	MinSamples int

	Local ClassConfig
	IO    ClassConfig
}

// Admission 是准入控制的实现。
type Admission struct {
	slowP50    time.Duration
	minSamples int
	local      *classBudget
	io         *classBudget
}

// metrics 是准入控制的可观测指标（进程级，按类标签区分）。
var (
	admittedTotal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "remilia", Subsystem: "admission", Name: "admitted_total",
		Help: "Events admitted by cost class",
	}, []string{"class"})).(*prometheus.CounterVec)
	rejectedTotal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "remilia", Subsystem: "admission", Name: "rejected_total",
		Help: "Events rejected by cost class",
	}, []string{"class"})).(*prometheus.CounterVec)
	queuedTotal = inframetrics.MustRegisterOrGet(nil, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "remilia", Subsystem: "admission", Name: "queued_total",
		Help: "Events that had to wait for a slot, by class",
	}, []string{"class"})).(*prometheus.CounterVec)
)

// New 构造准入控制实例。
func New(cfg Config) *Admission {
	if cfg.SlowP50 <= 0 {
		cfg.SlowP50 = 500 * time.Millisecond
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 3
	}
	return &Admission{
		slowP50:    cfg.SlowP50,
		minSamples: cfg.MinSamples,
		local:      newClassBudget(cfg.Local),
		io:         newClassBudget(cfg.IO),
	}
}

// Middleware 返回接入引擎的中间件。
func (a *Admission) Middleware() eventctx.Middleware { return a.handle }

// classOf 解析当前 matcher 的成本类别。
func (a *Admission) classOf(ctx *eventctx.Context) eventctx.CostClass {
	return ctx.MatcherCost().Resolve(a.slowP50, a.minSamples)
}

func (a *Admission) handle(next eventctx.Handler) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		class := a.classOf(ctx)
		budget := a.local
		if class == eventctx.CostIO {
			budget = a.io
		}
		if budget == nil {
			admittedTotal.WithLabelValues(class.String()).Inc()
			return next(ctx)
		}

		release, waited, ok := budget.acquire(ctx)
		if !ok {
			rejectedTotal.WithLabelValues(class.String()).Inc()
			return eventctx.ErrBusy
		}
		if waited {
			queuedTotal.WithLabelValues(class.String()).Inc()
		}
		defer release()
		admittedTotal.WithLabelValues(class.String()).Inc()
		return next(ctx)
	}
}

// classBudget 是单个类别的并发预算。
type classBudget struct {
	limit    int
	queue    int
	waitTime time.Duration

	sema    chan struct{}
	waiters atomic.Int64
}

func newClassBudget(cfg ClassConfig) *classBudget {
	if cfg.Limit <= 0 {
		return nil
	}
	b := &classBudget{limit: cfg.Limit, queue: cfg.Queue, waitTime: cfg.WaitTimeout}
	b.sema = make(chan struct{}, cfg.Limit)
	return b
}

// acquire 尝试获取一个槽位。
//
// 返回 release 用于释放槽位；waited 表示是否发生过排队；ok=false 表示应拒绝。
func (b *classBudget) acquire(ctx *eventctx.Context) (release func(), waited bool, ok bool) {
	select {
	case b.sema <- struct{}{}:
		return func() { <-b.sema }, false, true
	default:
	}

	if b.queue <= 0 {
		return nil, false, false
	}
	if b.waiters.Add(1) > int64(b.queue) {
		b.waiters.Add(-1)
		return nil, false, false
	}
	defer b.waiters.Add(-1)

	var timeout <-chan time.Time
	if b.waitTime > 0 {
		timer := time.NewTimer(b.waitTime)
		defer timer.Stop()
		timeout = timer.C
	}

	select {
	case b.sema <- struct{}{}:
		return func() { <-b.sema }, true, true
	case <-ctx.Context().Done():
		return nil, true, false
	case <-timeout:
		return nil, true, false
	}
}
