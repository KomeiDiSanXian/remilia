package context

// context.go — Context 结构体定义与核心生命周期

import (
	stdctx "context"
	"maps"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// extensionState 是面向用户的字符串键扩展容器。
type extensionState struct {
	mu sync.RWMutex
	m  map[string]any
}

func newStateExt() *extensionState {
	return &extensionState{m: make(map[string]any)}
}

// Matcher 定义 Matcher 的最小接口，用于避免循环依赖
type Matcher interface {
	GetSource() string
}

// ctxHolder 包装 context.Context，使 atomic.Pointer 能存储不同类型的 context。
type ctxHolder struct {
	Context stdctx.Context
}

// Context 上下文。每事件新鲜分配，不复用。
type Context struct {
	// 标准库 context，由中间件通过 SetStdContext 注入
	stdCtx atomic.Pointer[ctxHolder]

	// cancel 由 Clone 设置，可调用 Cancel() 主动释放 Clone 的 context。
	// 受 cancelMu 保护：Cancel 可能被多个 goroutine 并发调用
	// （如池任务收尾 + 调用方主动取消）。
	cancelMu sync.Mutex
	cancel   stdctx.CancelFunc

	matcher        Matcher
	platformEvent  platform.Event
	platformSender platform.Sender
	platformCaps   platform.Capabilities
	botID          string
	botName        string

	// dispatcher 是出站任务调度器（Engine 注入），用于异步发送消息
	dispatcher Dispatcher

	extInitialized atomic.Bool
	extMu          sync.Mutex
	extensions     *Extensions

	contentOnce sync.Once
	content     string

	// pendingRuleEffects 收集规则在 Match 过程中登记的延迟副作用
	// （如 OnCooldown 的冷却写入）。由引擎在当前 matcher 的全部规则
	// 通过后统一提交，任一规则失败则丢弃。
	//
	// ruleEffectSession 标记"引擎正在为某个 matcher 求值规则"：
	// 只有会话打开时 [Context.DeferRuleEffect] 才登记，会话外（FSM 直接求值、
	// 业务代码手工调用规则、handler 内）的登记被丢弃，不会漏进后续 matcher
	// 的提交窗口。会话由引擎开启、由提交/丢弃关闭。
	//
	// 写入者约定：规则由引擎 goroutine 同步调用（或 [WithTimeout] 在超时前
	// 返回后由 adoptRuleEffects 并入），因此本字段的读写始终发生在引擎
	// goroutine 上。超时后仍在运行的规则 goroutine 工作在派生 Context 上，
	// 登记项不会进入这里。不参与 Clone。
	ruleEffectSession  atomic.Bool
	pendingRuleEffects []func()
}

// DeferRuleEffect 登记一个延迟到"当前 matcher 确认命中"才执行的规则副作用。
//
// 框架内部机制（参见 [OnCooldown]）：规则本身保持纯函数，把写状态动作放进 fn；
// 引擎在该 matcher 的全部规则通过后调用 [Context.CommitPendingRuleEffects]
// 统一执行，任一规则失败则调用 [Context.DiscardPendingRuleEffects] 丢弃。
//
// 只有引擎正在为某个 matcher 求值规则时（BeginRuleEffectSession 与提交/丢弃
// 之间）才会真正登记：其它路径直接调用带副作用的规则（如 FSM 的 Event.Match、
// 业务代码手工求值）不会污染当前 Context，避免登记项被后续某个无关 matcher
// 的提交执行（文档承诺"这些场景不执行副作用"，此前实现会漏进提交窗口）。
func (ctx *Context) DeferRuleEffect(fn func()) {
	if ctx == nil || fn == nil {
		return
	}
	if !ctx.ruleEffectSession.Load() {
		return
	}
	ctx.pendingRuleEffects = append(ctx.pendingRuleEffects, fn)
}

// BeginRuleEffectSession 打开一次匹配会话的副作用登记（框架内部）。
//
// 引擎在调用 Matcher.Match 前打开；[Context.CommitPendingRuleEffects] /
// [Context.DiscardPendingRuleEffects] 关闭会话。
func (ctx *Context) BeginRuleEffectSession() {
	if ctx == nil {
		return
	}
	ctx.ruleEffectSession.Store(true)
}

// CommitPendingRuleEffects 执行并清空所有已登记的延迟规则副作用，
// 并关闭本次匹配会话的登记。由引擎在 matcher 全部规则通过后调用（框架内部）。
func (ctx *Context) CommitPendingRuleEffects() {
	if ctx == nil {
		return
	}
	ctx.ruleEffectSession.Store(false)
	if len(ctx.pendingRuleEffects) == 0 {
		return
	}
	effects := ctx.pendingRuleEffects
	ctx.pendingRuleEffects = nil
	for _, fn := range effects {
		fn()
	}
}

// DiscardPendingRuleEffects 丢弃所有已登记的延迟规则副作用，
// 并关闭本次匹配会话的登记。由引擎在 matcher 匹配失败后调用（框架内部）。
func (ctx *Context) DiscardPendingRuleEffects() {
	if ctx == nil {
		return
	}
	ctx.ruleEffectSession.Store(false)
	ctx.pendingRuleEffects = nil
}

// adoptRuleEffects 把派生 Context 上登记的延迟副作用并入当前匹配会话。
//
// 供 [WithTimeout] 在包装的规则于超时前返回时调用：该规则在派生 Context
// 上执行（避免超时返回后与引擎推进的匹配会话互相污染），副作用需要搬回
// 本次匹配的 Context 才会被引擎提交。
//
// 只在规则 goroutine 已经结束（收到结果）后调用；超时路径不搬运——
// 迟到登记的副作用留在派生 Context 里，随副本一起被 GC。
func (ctx *Context) adoptRuleEffects(from *Context) {
	if ctx == nil || from == nil || len(from.pendingRuleEffects) == 0 {
		return
	}
	ctx.pendingRuleEffects = append(ctx.pendingRuleEffects, from.pendingRuleEffects...)
	from.pendingRuleEffects = nil
}

// Cancel 主动释放 Clone 创建的 context 资源。
// 对非 Clone 的 Context 调用无效果；可被并发调用（幂等）。
// 不调用也不会泄露 — GC 会在 Context 不可达后自动清理。
func (ctx *Context) Cancel() {
	if ctx == nil {
		return
	}
	// 取走撤销函数再调用：并发 Cancel 只有一个真正执行，且不会读写竞争
	ctx.cancelMu.Lock()
	cancel := ctx.cancel
	ctx.cancel = nil
	ctx.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// SetDispatcher 设置出站调度器（框架内部，由 Engine 在 ProcessPlatformEvent 时注入）。
func (ctx *Context) SetDispatcher(d Dispatcher) {
	if ctx == nil {
		return
	}
	ctx.dispatcher = d
}

// SetMatcher 设置当前命中的 Matcher（框架内部，由 Engine 在 processEventContext 中注入）
//
// 使用已有的 context.Matcher 接口存储，无需任何堆分配：
// *engine.Matcher 是指针类型，可直接内联存储于接口的 data word，完全 alloc-free。
func (ctx *Context) SetMatcher(m Matcher) {
	if ctx == nil {
		return
	}
	ctx.matcher = m
}

// SetPlatformCapabilities 设置平台能力（框架内部，由 Engine.ProcessPlatformEvent 注入）
func (ctx *Context) SetPlatformCapabilities(caps platform.Capabilities) {
	if ctx == nil {
		return
	}
	ctx.platformCaps = caps
}

// Context 返回标准库 context.Context
func (ctx *Context) Context() stdctx.Context {
	if ctx == nil {
		return stdctx.Background()
	}
	h := ctx.stdCtx.Load()
	if h == nil {
		return stdctx.Background()
	}
	return h.Context
}

// SetStdContext 设置标准库 context。
//
// 仅供中间件使用：注入 trace context（OpenTelemetry）或超时/deadline 控制。
func (ctx *Context) SetStdContext(stdCtx stdctx.Context) {
	if ctx == nil {
		return
	}
	if stdCtx == nil {
		stdCtx = stdctx.Background()
	}
	ctx.stdCtx.Store(&ctxHolder{Context: stdCtx})
}

// copyExtensions 把 src 的扩展存储复制到 dst。
//
// # 复制语义（契约）
//
// 此前注释自称"深拷贝"，与实现不符；实际语义如下：
//
//   - 字符串键存储（ctx.Set/Get 的 extensionState）：整表拷贝，
//     两侧后续写入互不影响；
//   - 类型键存储（Ext().Set/SetTyped）：逐条复制**引用**，值本身与 src 共享。
//     可变值（切片/映射/指针）在两侧是同一个对象：需要修改时应先自行拷贝，
//     或依赖该类型自带的锁（如 *extensionState 自带 mutex）。
//
// 因此 Clone 适合"读隔离 + 独立写入"：读取 src 已写入的数据安全，
// 但直接改写共享的可变值会同时影响两边（可能并发访问）。
//
// 空存储快速路径：src 从未初始化扩展（extInitialized == false）时直接返回。
// 此前无条件调用 src.Ext()/dst.Ext() 会触发两侧的惰性初始化，并为从未使用
// 扩展的 Context（纯回复 handler 的常见形态）白白分配 Extensions + map +
// Snapshot 整表拷贝——这是每事件 Clone 的分配大头（基准实测 7 allocs 中占 5）。
// 类型键存储改为在读锁下逐条拷贝，替代 Snapshot 整表分配。
func copyExtensions(src, dst *Context) {
	if src == nil || !src.extInitialized.Load() {
		return
	}

	ex := src.extensions
	extStateType := extTypeOf[*extensionState]()

	dstExt := dst.Ext()
	ex.mu.RLock()
	for k, v := range ex.m {
		if k == extStateType {
			continue
		}
		dstExt.Set(k, v)
	}
	ex.mu.RUnlock()

	if s, ok := ex.GetTyped[*extensionState](); ok && s != nil {
		s.mu.RLock()
		cp := make(map[string]any, len(s.m))
		maps.Copy(cp, s.m)
		s.mu.RUnlock()
		dstExt.SetTyped(&extensionState{m: cp})
	}
}

// cloneBase 创建 Context 的基础拷贝（共享字段），不含扩展存储。
// 克隆始终独立于原始 Context：不会被原始的 cancel 影响。
func (ctx *Context) cloneBase() *Context {
	base := &Context{
		matcher:        ctx.matcher,
		platformEvent:  ctx.platformEvent,
		platformSender: ctx.platformSender,
		platformCaps:   ctx.platformCaps,
		botID:          ctx.botID,
		botName:        ctx.botName,
		dispatcher:     ctx.dispatcher,
	}

	var indepCtx stdctx.Context
	var cancel stdctx.CancelFunc
	if deadline, ok := ctx.Context().Deadline(); ok {
		indepCtx, cancel = stdctx.WithDeadline(stdctx.Background(), deadline)
	} else {
		indepCtx, cancel = stdctx.WithCancel(stdctx.Background())
	}
	base.cancel = cancel

	// 匹配会话标记随克隆继承：WithTimeout 的派生 Context 也要能登记副作用
	// （规则在超时前返回时由 adoptRuleEffects 并回原 Context）。
	base.ruleEffectSession.Store(ctx.ruleEffectSession.Load())

	if span := trace.SpanFromContext(ctx.Context()); span.SpanContext().IsValid() {
		indepCtx = trace.ContextWithSpan(indepCtx, span)
	}
	base.SetStdContext(indepCtx)

	return base
}

// Clone 克隆 Context 用于异步操作。
//
// 复制语义见 [copyExtensions]：字符串键存储整表拷贝、类型键存储共享值引用；
// 平台事件/发送器等只读字段直接共享，std context 独立派生（可用 [Context.Cancel]
// 提前释放）。因此适合"读隔离 + 独立写入"，不适合把共享的可变值当作私有副本。
func (ctx *Context) Clone() *Context {
	newCtx := ctx.cloneBase()
	copyExtensions(ctx, newCtx)
	return newCtx
}

// Ext 返回类型键扩展存储。
func (ctx *Context) Ext() *Extensions {
	if ctx == nil {
		return nil
	}
	if ctx.extInitialized.Load() {
		return ctx.extensions
	}
	ctx.extMu.Lock()
	defer ctx.extMu.Unlock()
	if !ctx.extInitialized.Load() {
		ctx.extensions = newExtensions()
		ctx.extInitialized.Store(true)
	}
	return ctx.extensions
}

// Tracer 返回当前事件的 OpenTelemetry Tracer。
//
// 从全局 TracerProvider 获取（通过 otel.GetTracerProvider()），
// 确保在 infra/tracing 初始化后能返回真实的追踪 tracer，
// 而非永远是 no-op。
//
// 使用场景：在 handler 中手动创建子 span：
//
//	tracer := ctx.Tracer()
//	_, span := tracer.Start(ctx.Context(), "my-operation")
//	defer span.End()
func (ctx *Context) Tracer() trace.Tracer {
	return otel.GetTracerProvider().Tracer("remilia")
}
