package remilia

import (
	"reflect"
	"sync"
)

// Extensions is a typed-key extension store.
//
// It is a concurrency-safe container mapping `reflect.Type` -> `any`.
//
// V2 direction:
//   - framework caches/metadata should be stored as private extension types
//   - user state will be implemented as a State extension, while keeping ctx.Set/ctx.Get sugar
//
// NOTE: this is introduced in Phase 1 for progressive migration.
// Existing V1 state/internalState still exist temporarily.
//
// (Updated: legacy V1 internal state layers have been removed; Extensions is the only framework metadata store.)
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
// Values are not deep-copied.
func (e *Extensions) Snapshot() map[reflect.Type]any {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[reflect.Type]any, len(e.m))
	for k, v := range e.m {
		out[k] = v
	}
	return out
}

// --- Generic helpers (package-level) ---

// extTypeOf returns the reflect.Type key for T.
func extTypeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
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
