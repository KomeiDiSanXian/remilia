package pool

import "sync"

// TypedPool is a type-safe, instrumented pool built on top of sync.Pool.
//
// It standardizes pool usage across the project:
//   - no interface{} casting at call sites
//   - unified Stats/Reset API
//   - consistent capacity bounding policies can be implemented at call sites
//
// NOTE: TypedPool does not enforce any capacity policy by itself; callers can decide
// whether to Put back based on stats or the value itself.
type TypedPool[T any] struct {
	p *InstrumentedPool
}

// NewTypedPool creates a new typed pool.
func NewTypedPool[T any](newFunc func() T) *TypedPool[T] {
	ip := NewInstrumentedPool(func() any { return newFunc() })
	return &TypedPool[T]{p: ip}
}

func (tp *TypedPool[T]) Get() T {
	v := tp.p.Get()
	// We never store values other than T, so this assertion is safe.
	return v.(T)
}

func (tp *TypedPool[T]) Put(v T) { tp.p.Put(v) }

func (tp *TypedPool[T]) Stats() PoolStats { return tp.p.Stats() }

func (tp *TypedPool[T]) Reset() { tp.p.Reset() }

// Raw exposes underlying sync.Pool for rare cases; avoid using it unless necessary.
func (tp *TypedPool[T]) Raw() *sync.Pool { return &tp.p.pool }
