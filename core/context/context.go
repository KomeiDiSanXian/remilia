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

	// cancel 由 Clone 设置，可调用 Cancel() 主动释放 Clone 的 context
	cancel stdctx.CancelFunc

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
}

// Cancel 主动释放 Clone 创建的 context 资源。
// 对非 Clone 的 Context 调用无效果。
// 不调用也不会泄露 — GC 会在 Context 不可达后自动清理。
func (ctx *Context) Cancel() {
	if ctx == nil {
		return
	}
	if ctx.cancel != nil {
		ctx.cancel()
		ctx.cancel = nil
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

// copyExtensions 将 src 的两套扩展存储（类型键 + 字符串键）深拷贝到 dst。
//
// 两套存储分别对应：
//   - 类型键（Extensions）：框架组件间强类型数据，跳过 *extensionState 条目（由字符串键路径单独拷贝）
//   - 字符串键（extensionState）：插件/handler 层通用键值对，做 map 深拷贝保证隔离
func copyExtensions(src, dst *Context) {
	if ex := src.Ext(); ex != nil {
		dstExt := dst.Ext()
		extStateType := extTypeOf[*extensionState]()
		for k, v := range ex.Snapshot() {
			if k == extStateType {
				continue
			}
			dstExt.Set(k, v)
		}
	}

	if s, ok := ExtGet[*extensionState](src.Ext()); ok && s != nil {
		s.mu.RLock()
		cp := make(map[string]any, len(s.m))
		maps.Copy(cp, s.m)
		s.mu.RUnlock()
		ExtSet(dst.Ext(), &extensionState{m: cp})
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

	if span := trace.SpanFromContext(ctx.Context()); span.SpanContext().IsValid() {
		indepCtx = trace.ContextWithSpan(indepCtx, span)
	}
	base.SetStdContext(indepCtx)

	return base
}

// Clone 克隆 Context 用于异步操作
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
