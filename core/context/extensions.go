package context

import (
	"maps"
	"reflect"
	"sync"
)

// Extensions is a typed-key extension store.
//
// It is a concurrency-safe container mapping `reflect.Type` -> `any`.
//
// # 两套状态 API 使用指南
//
// Context 提供两套完全隔离、互不影响的键值存储机制，请根据场景选择：
//
// ## 1. 字符串键 API（推荐，适合插件/handler 层）
//
//		ctx.Set("user_id", uid)
//		v, ok := ctx.Get("user_id")
//
//	  - 键类型：string
//	  - 用途：在同一事件的不同 handler 或中间件之间传递临时状态
//	  - 推荐给：插件开发者、普通中间件开发者
//
// ## 2. 类型键 API（框架内部使用）
//
//		context.ExtSet(ctx.Ext(), myTypedValue{})
//		val, ok := context.ExtGet[myTypedValue](ctx.Ext())
//
//	  - 键类型：reflect.Type（通过泛型参数隐式传入）
//	  - 用途：框架组件间共享强类型数据（如 retryMetadata、middlewareTrace、parsedCommand）
//	  - 优点：零字符串分配、编译期类型检查、不可能与字符串键冲突
//	  - 适用于：框架级中间件开发者（如实现新的 core 中间件）
//	  - 插件开发者一般不需要直接使用此 API
//
// ## 隔离性保证
//
// 两套系统使用完全不同的底层存储（字符串 map vs reflect.Type map），
// ctx.Set("parsed_command", v) 不会与框架通过 ExtSet 存储的 parsedCommand 产生任何冲突。
type Extensions struct {
	mu sync.RWMutex
	m  map[reflect.Type]any
}

func newExtensions() *Extensions {
	return &Extensions{m: make(map[reflect.Type]any)}
}

// Get returns the extension value by its type.
func (e *Extensions) Get(t reflect.Type) (any, bool) {
	if e == nil {
		return nil, false
	}
	e.mu.RLock()
	v, ok := e.m[t]
	e.mu.RUnlock()
	return v, ok
}

// Set sets the extension value by its type.
func (e *Extensions) Set(t reflect.Type, v any) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.m[t] = v
	e.mu.Unlock()
}

// GetOrInit returns the extension value by type, or initializes and stores it.
func (e *Extensions) GetOrInit(t reflect.Type, init func() any) any {
	if e == nil {
		return nil
	}

	// Fast path: read lock
	e.mu.RLock()
	v, ok := e.m[t]
	e.mu.RUnlock()
	if ok {
		return v
	}

	// Slow path: write lock
	e.mu.Lock()
	defer e.mu.Unlock()
	if v, ok := e.m[t]; ok {
		return v
	}
	nv := init()
	e.m[t] = nv
	return nv
}

// Snapshot returns a shallow copy of the current extension map.
func (e *Extensions) Snapshot() map[reflect.Type]any {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[reflect.Type]any, len(e.m))
	maps.Copy(out, e.m)
	return out
}

// Clear removes all extensions from the container.
// This is used for cleaning up contexts before returning them to the pool.
func (e *Extensions) Clear() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Clear the map
	for k := range e.m {
		delete(e.m, k)
	}
}

// --- Generic helpers (package-level) ---

// extTypeOf returns the reflect.Type key for T.
func extTypeOf[T any]() reflect.Type {
	return reflect.TypeFor[T]()
}

// ExtGet reads a typed extension value.
func ExtGet[T any](e *Extensions) (T, bool) {
	var zero T
	if e == nil {
		return zero, false
	}
	v, ok := e.Get(extTypeOf[T]())
	if !ok {
		return zero, false
	}
	tv, ok := v.(T)
	if !ok {
		return zero, false
	}
	return tv, true
}

// ExtSet stores a typed extension value.
func ExtSet[T any](e *Extensions, v T) {
	if e == nil {
		return
	}
	e.Set(extTypeOf[T](), v)
}

// ExtGetOrInit returns a typed extension value or initializes it once.
func ExtGetOrInit[T any](e *Extensions, init func() T) T {
	var zero T
	if e == nil {
		return zero
	}
	v := e.GetOrInit(extTypeOf[T](), func() any { return init() })
	tv, ok := v.(T)
	if !ok {
		return zero
	}
	return tv
}
