package engine

import (
	"context"
	"slices"
	"sync"
)

type runtime struct {
	mu sync.Mutex

	// registered components (best-effort). Order doesn't matter.
	components []runtimeComponent
}

func (rt *runtime) register(c runtimeComponent) {
	if c == nil {
		return
	}
	p := rt.components
	// small optimization: avoid duplicates by pointer equality
	if slices.Contains(p, c) {
		return
	}
	rt.components = append(rt.components, c)
}

func (rt *runtime) stopAll() {
	for _, c := range rt.components {
		c.stop()
	}
}

func (rt *runtime) waitAll(ctx context.Context) error {
	for _, c := range rt.components {
		if err := c.wait(ctx); err != nil {
			return err
		}
	}
	return nil
}
