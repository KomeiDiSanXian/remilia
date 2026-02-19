package engine

import (
	"strings"

	"github.com/KomeiDiSanXian/remilia/core/context"
)

// RebuildMatcherChain 重新为给定 matcher 组合全局/插件/局部中间件链
//
// 此方法会获取读锁，适用于外部调用（向后兼容）
func (e *Engine) RebuildMatcherChain(m *Matcher) {
	if m == nil {
		return
	}
	e.rebuildMatcherChainCOW(m)
}

// rebuildMatcherChainCOW 重新为给定 matcher 组合全局/插件/局部中间件链（COW 版本）
//
// 使用代际号避免全量重建，按需合并
func (e *Engine) rebuildMatcherChainCOW(m *Matcher) {
	if m == nil {
		return
	}

	mwState := e.middleware.Load()
	e.ensureMatcherChainWithState(m, mwState)
}

// ensureMatcherChainWithState 检查缓存是否过期，必要时惰性合并中间件链
func (e *Engine) ensureMatcherChainWithState(m *Matcher, mwState *middlewareState) {
	if m == nil || mwState == nil {
		return
	}

	// groupName must be explicitly set (e.g. by plugin manager / BasePlugin.AddMatcher).
	// Source is for diagnostics/labeling only and must NOT drive group behavior.
	groupName := m.group

	groupSnap := mwState.groupMiddlewares[groupName]
	globalSnap := mwState.global
	var groupChain []Middleware
	var groupGen uint64
	if groupSnap != nil {
		groupChain = groupSnap.chain
		groupGen = groupSnap.gen
	}

	m.ensureChain(globalSnap.chain, globalSnap.gen, groupChain, groupGen)
}

// Use 注册全局处理器中间件（COW 写操作）
//
// 中间件按添加顺序链式包裹
func (e *Engine) Use(mw ...Middleware) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldMwState := e.middleware.Load() // 无需类型断言

	// 复制状态并追加中间件，递增代际号
	newMwState := copyMiddlewareState(oldMwState)
	newChain := append([]Middleware(nil), newMwState.global.chain...)
	newChain = append(newChain, mw...)
	newMwState.global.chain = newChain
	newMwState.global.gen++

	e.middleware.Store(newMwState)

	return e
}

// UseForGroup 为指定分组注册中间件（COW 写操作）
//
// 仅该分组注册的 matcher 生效
func (e *Engine) UseForGroup(groupName string, mw ...Middleware) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	key := strings.TrimSpace(groupName)
	if key == "" {
		return e
	}

	oldMwState := e.middleware.Load() // 无需类型断言

	// 复制状态并更新目标分组快照
	newMwState := copyMiddlewareState(oldMwState)
	snap, ok := newMwState.groupMiddlewares[key]
	if !ok {
		snap = &middlewareSnapshot{chain: make([]Middleware, 0), gen: 1}
		newMwState.groupMiddlewares[key] = snap
	}
	newChain := append([]Middleware(nil), snap.chain...)
	newChain = append(newChain, mw...)
	snap.chain = newChain
	snap.gen++

	e.middleware.Store(newMwState)

	return e
}

// ResetMiddlewares 清空全局与插件级中间件（COW 写操作）
// 不影响已注册的 matcher 局部中间件
func (e *Engine) ResetMiddlewares() *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 创建新的空中间件状态
	newMwState := newMiddlewareState()

	// 原子替换
	e.middleware.Store(newMwState)

	return e
}

// Named wraps a middleware with a name and triggers trace hook if configured.
func (e *Engine) Named(name string, mw Middleware) Middleware {
	name = strings.TrimSpace(name)
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			mwState := e.middleware.Load()
			if mwState.traceHook != nil && name != "" {
				func() {
					defer func() { _ = recover() }()
					(*mwState.traceHook)(name, ctx)
				}()
			}
			return mw(next)(ctx)
		}
	}
}

// SetMiddlewareTraceHook sets a hook to be called when named middleware executes.
// Set to nil to disable tracing.
func (e *Engine) SetMiddlewareTraceHook(hook MiddlewareTraceHook) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldMwState := e.middleware.Load() // 无需类型断言
	newMwState := copyMiddlewareState(oldMwState)

	if hook == nil {
		newMwState.traceHook = nil
	} else {
		newMwState.traceHook = &hook
	}

	e.middleware.Store(newMwState)
	return e
}

// EnableMiddlewareTrace enables automatic recording of executed named middleware
// into ctx extensions (for debugging and monitoring).
func (e *Engine) EnableMiddlewareTrace() *Engine {
	return e.SetMiddlewareTraceHook(func(name string, ctx *context.Context) {
		if trace, ok := ctx.GetMiddlewareTrace(); ok {
			ctx.SetMiddlewareTrace(append(trace, name))
		} else {
			ctx.SetMiddlewareTrace([]string{name})
		}
	})
}
