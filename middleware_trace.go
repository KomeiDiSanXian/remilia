package remilia

import "strings"

// MiddlewareTraceHook is called when a *named middleware is actually executed* (runtime).
//
// Contract (runtime semantics):
//   - The hook is invoked once per execution of a middleware wrapped by Engine.Named.
//   - The order reflects the real execution order of middlewares for that event.
//   - If a middleware is skipped due to early return/short-circuiting, it will NOT appear.
//   - This is intentionally NOT a "static chain" snapshot.
//
// name: middleware name passed to Engine.Named
// ctx:  current Context
//
// Hook must be fast and must not panic.
// If it panics, Engine will recover and continue executing handler.
type MiddlewareTraceHook func(name string, ctx *Context)

// SetMiddlewareTraceHook sets a runtime trace hook.
//
// Notes:
//   - This affects only named middlewares created via Engine.Named.
//   - Hook is stored in middlewareState (COW), so reads are lock-free.
//   - Passing nil disables the hook.
func (e *Engine) SetMiddlewareTraceHook(hook MiddlewareTraceHook) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	old := e.middleware.Load().(*middlewareState)
	newState := copyMiddlewareState(old)
	newState.traceHook = hook
	e.middleware.Store(newState)
	return e
}

// EnableMiddlewareTrace is a convenience method that enables runtime trace recording.
//
// It records the actual executed named middlewares into Context internal state.
// The trace is stored as []string under an internal-only key.
func (e *Engine) EnableMiddlewareTrace() *Engine {
	return e.SetMiddlewareTraceHook(func(name string, ctx *Context) {
		if ctx == nil {
			return
		}
		if strings.TrimSpace(name) == "" {
			return
		}

		if arr, ok := ctx.GetMiddlewareTrace(); ok {
			arr = append(arr, name)
			ctx.SetMiddlewareTrace(arr)
			return
		}
		ctx.SetMiddlewareTrace([]string{name})
	})
}

// DisableMiddlewareTrace disables any trace hook.
func (e *Engine) DisableMiddlewareTrace() *Engine { return e.SetMiddlewareTraceHook(nil) }
