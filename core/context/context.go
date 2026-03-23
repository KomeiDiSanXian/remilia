package context

// context.go — Context 结构体定义与核心生命周期
//
// 职责划分：
//   - context.go    — 结构体定义、Clone / SetStdContext / Ext / Tracer
//   - decode.go     — 热路径缓存（GetMessageContent、GetSenderInfo），平台无关
//   - state.go      — 字符串键扩展状态（Set/Get/Delete/All 及类型便捷方法）
//   - metadata.go   — 框架元数据（RetryAttempt、MiddlewareTrace、ParsedCommand、MatcherSource）
//   - extensions.go — 类型键扩展存储（ExtGet/ExtSet/ExtGetOrInit）
//   - permission.go — 权限桥接（GetPermissionManager/SetPermissionManager）
//   - pool.go       — Context 对象池（ReleaseContext）

import (
	stdctx "context"
	"maps"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// extensionState is the user-facing string-keyed extension container.
//
// It is intentionally unexported. Access is via ctx.Set/ctx.Get/ctx.All.
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

// Context 上下文
type Context struct {
	ctxMu   sync.RWMutex   // 保护 ctx 字段的读写锁
	ctx     stdctx.Context // 标准库 context，用于超时控制、取消传播等
	matcher Matcher        // Matcher 引用（使用 interface{} 避免循环依赖）

	// --- 平台无关字段（新路径）---
	// 由 AcquireContextFromEvent 填充
	platformEvent  platform.Event        // 平台无关事件抽象
	platformSender platform.Sender       // 平台无关消息发送器
	platformCaps   platform.Capabilities // 平台能力声明（由 Engine 注入）
	botID          string                // 机器人自身平台 ID（由 Engine 注入，供 IsFromSelf 使用）

	extInitialized atomic.Bool
	extMu          sync.Mutex
	extensions     *Extensions

	// --- hot-path field caches（通用）---
	contentOnce sync.Once
	content     string // cached GetMessageContent result
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
		logger.Error("[Context] CRITICAL: Context receiver is nil, returning Background()")
		return stdctx.Background()
	}
	ctx.ctxMu.RLock()
	c := ctx.ctx
	ctx.ctxMu.RUnlock()
	if c == nil {
		logger.Warn("[Context] stdctx is unexpectedly nil, returning Background(). This may indicate a bug.")
		return stdctx.Background()
	}
	return c
}

// SetStdContext 设置标准库 context。
//
// 仅供中间件使用：注入 trace context（OpenTelemetry）或超时/deadline 控制。
func (ctx *Context) SetStdContext(stdCtx stdctx.Context) {
	if ctx == nil {
		logger.Error("[Context] CRITICAL: Cannot call SetStdContext on nil receiver")
		return
	}
	if stdCtx == nil {
		logger.Warn("[Context] Attempting to set nil stdctx, using Background() instead")
		stdCtx = stdctx.Background()
	}
	ctx.ctxMu.Lock()
	ctx.ctx = stdCtx
	ctx.ctxMu.Unlock()
}

// Clone 克隆 Context 用于异步操作
func (ctx *Context) Clone() *Context {
	newStdCtx := stdctx.Background()

	if deadline, ok := ctx.Context().Deadline(); ok {
		var cancel stdctx.CancelFunc
		newStdCtx, cancel = stdctx.WithDeadline(newStdCtx, deadline)
		_ = cancel
	}

	if span := trace.SpanFromContext(ctx.Context()); span.SpanContext().IsValid() {
		newStdCtx = trace.ContextWithSpan(newStdCtx, span)
	}

	newCtx := &Context{
		ctx:            newStdCtx,
		matcher:        ctx.matcher,
		platformEvent:  ctx.platformEvent,  // 保留平台无关事件引用
		platformSender: ctx.platformSender, // 保留平台发送器，使 Reply() 在克隆后仍可用
		platformCaps:   ctx.platformCaps,   // B1 fix: 保留平台能力声明，避免克隆后渐进增强失效
	}

	if ex := ctx.Ext(); ex != nil {
		dst := newCtx.Ext()
		// *extensionState 使用下方的深拷贝路径，此处跳过避免无效的浅拷贝写入
		extStateType := extTypeOf[*extensionState]()
		for k, v := range ex.Snapshot() {
			if k == extStateType {
				continue
			}
			dst.Set(k, v)
		}
	}

	if s, ok := ExtGet[*extensionState](ctx.Ext()); ok && s != nil {
		s.mu.RLock()
		cp := make(map[string]any, len(s.m))
		maps.Copy(cp, s.m)
		s.mu.RUnlock()
		ExtSet(newCtx.Ext(), &extensionState{m: cp})
	}

	return newCtx
}

// Ext returns the typed-key extensions store.
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
