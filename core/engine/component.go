package engine

import "context"

// engineComponent represents an internal Engine runtime component.
//
// It is deliberately unexported to avoid expanding the public API surface.
// The goal is to keep Engine's core responsibilities (routing/matching) separate
// from optional runtime background workloads (cleaners, async processors, etc.).
//
// Contract:
//   - stop() must be safe to call multiple times.
//   - stop() should return quickly (signal-based) and leave any waiting to engine shutdown.
//   - wait(ctx) must wait until the component is fully stopped or ctx is done.
//
// NOTE: components are engine-internal; callers should use Engine.Shutdown(ctx).
type engineComponent interface {
	stop()
	wait(ctx context.Context) error
}
