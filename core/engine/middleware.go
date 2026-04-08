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
	var groupChain []context.Middleware
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
func (e *Engine) Use(mw ...context.Middleware) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldMwState := e.middleware.Load() // 无需类型断言

	// 复制状态并追加中间件，递增代际号
	newMwState := copyMiddlewareState(oldMwState)
	newChain := append([]context.Middleware(nil), newMwState.global.chain...)
	newChain = append(newChain, mw...)
	newMwState.global.chain = newChain
	newMwState.global.gen++

	e.middleware.Store(newMwState)

	// 失效所有 matcher 的缓存，确保它们重建中间件链
	state := e.state.Load()
	for _, m := range state.matchers {
		if m != nil {
			m.invalidateCombinedChain()
		}
	}

	return e
}

// UseForGroup 为指定分组注册中间件（COW 写操作）
//
// 仅该分组注册的 matcher 生效
func (e *Engine) UseForGroup(groupName string, mw ...context.Middleware) *Engine {
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
		snap = &middlewareSnapshot{chain: make([]context.Middleware, 0), gen: 1}
		newMwState.groupMiddlewares[key] = snap
	}
	newChain := append([]context.Middleware(nil), snap.chain...)
	newChain = append(newChain, mw...)
	snap.chain = newChain
	snap.gen++

	e.middleware.Store(newMwState)

	// 失效该组所有 matcher 的缓存，确保它们重建中间件链
	// 这样可以避免 matcher 使用过期的中间件
	state := e.state.Load()
	if groupMatchers, ok := state.groupIndex[key]; ok {
		for _, m := range groupMatchers {
			if m != nil {
				m.invalidateCombinedChain()
			}
		}
	}

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

// ResetGroupMiddleware 清除指定分组的所有中间件（COW 写操作）。
//
// 通常与 UseForGroup 配合使用（先清除旧链，再注册新链），以实现幂等的分组中间件更新。
// 典型场景：
//   - 插件卸载时清除残留的守卫中间件，防止后续重新注册该插件时产生重复执行
//   - pluginctrl 的 autoWireListener 在 OnPluginLoaded 前先 Reset，保证每次只注册一个守卫
//
// 若指定分组不存在中间件条目，此操作为 no-op。
func (e *Engine) ResetGroupMiddleware(groupName string) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	key := strings.TrimSpace(groupName)
	if key == "" {
		return e
	}

	oldMwState := e.middleware.Load()
	// 分组无条目时快速返回，避免不必要的 COW 复制
	if _, exists := oldMwState.groupMiddlewares[key]; !exists {
		return e
	}

	newMwState := copyMiddlewareState(oldMwState)
	delete(newMwState.groupMiddlewares, key)
	e.middleware.Store(newMwState)

	// 失效该组中仍存活的 Matcher 缓存（理论上卸载时已无 Matcher，但防御性处理）
	state := e.state.Load()
	if groupMatchers, ok := state.groupIndex[key]; ok {
		for _, m := range groupMatchers {
			if m != nil {
				m.invalidateCombinedChain()
			}
		}
	}

	return e
}

// Named wraps a middleware with a name and triggers trace hook if configured.
func (e *Engine) Named(name string, mw context.Middleware) context.Middleware {
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
