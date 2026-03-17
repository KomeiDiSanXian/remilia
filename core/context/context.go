package context

// context.go — Context 结构体定义与核心生命周期
//
// 职责划分（拆分后）：
//   - context.go    — 结构体定义、NewContext / Clone / SetStdContext / Ext / Tracer
//   - decode.go     — 事件解码（DecodeEvent）、热路径缓存（GetMessageContent、GetAuthor）、消息发送
//   - state.go      — 字符串键扩展状态（Set/Get/Delete/All 及类型便捷方法）
//   - metadata.go   — 框架元数据（RetryAttempt、MiddlewareTrace、ParsedCommand、MatcherSource）
//   - extensions.go — 类型键扩展存储（ExtGet/ExtSet/ExtGetOrInit）
//   - permission.go — 权限桥接（GetPermissionManager/SetPermissionManager）
//   - pool.go       — Context 对象池（AcquireContext/ReleaseContext）

import (
	stdctx "context"
	"maps"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
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

// decodeCache is a typed union that holds the result of the first successful
// DecodeEvent call for this Context.  Using concrete fields instead of `any`
// eliminates interface boxing, avoids a reflect.Type string allocation on
// every call, and lets the GC scan the value without an extra indirection.
//
// Only one field is populated at a time; which one is indicated by kind.
type decodeCache struct {
	kind uint8 // 0=empty 1=C2C 2=GroupAt 3=generic

	c2c     dto.C2CMessageCreateEvent
	groupAt dto.GroupAtMessageCreateEvent
	generic any // fallback for other event types
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
	event   *dto.Payload

	// --- 平台无关字段（新路径）---
	// 由 AcquireContextFromEvent 填充，旧路径保持 nil
	platformEvent  platform.Event  // 平台无关事件抽象
	platformSender platform.Sender // 平台无关消息发送器

	extInitialized atomic.Bool
	extMu          sync.Mutex
	extensions     *Extensions

	api openapi.OpenAPI

	// --- decode cache (typed union, replaces any+string) ---
	// Protected by decodeMu. A Context is processed by one handler chain at
	// a time, so contention is essentially zero.
	decodeMu sync.Mutex
	decoded  decodeCache

	// --- hot-path field caches ---
	// These are populated lazily on first access and never cleared until
	// ReleaseContext; they avoid repeated gjson.GetBytes calls when multiple
	// matchers/handlers inspect the same field.
	contentOnce sync.Once
	content     string // cached GetMessageContent result

	authorOnce sync.Once
	author     *dto.Author // cached GetAuthor result
}

// NewContext 创建一个新的上下文
func NewContext(event *dto.Payload, api openapi.OpenAPI) *Context {
	return &Context{
		ctx:   stdctx.Background(),
		event: event,
		api:   api,
	}
}

// NewContextWithContext 创建带自定义标准库 context 的上下文
// 用于中间件需要注入自定义 context 的场景（如注入 trace context）
func NewContextWithContext(ctx stdctx.Context, event *dto.Payload, api openapi.OpenAPI) *Context {
	c := NewContext(event, api)
	c.SetStdContext(ctx)
	return c
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
	var clonedEvent *dto.Payload
	if ctx.event != nil {
		clonedEvent = ctx.event.Clone()
	}

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
		event:          clonedEvent,
		api:            ctx.api,
		platformEvent:  ctx.platformEvent,  // 保留平台无关事件引用
		platformSender: ctx.platformSender, // 保留平台发送器，使 Reply() 在克隆后仍可用
	}

	if ex := ctx.Ext(); ex != nil {
		dst := newCtx.Ext()
		for k, v := range ex.Snapshot() {
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
