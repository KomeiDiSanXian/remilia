package engine

import (
	"context"
	"sync"
)

type engineRuntime struct {
	mu sync.Mutex

	// registered components (best-effort). Order doesn't matter.
	components []runtimeComponent

	// eventDone is a channel closed when all in-flight events finish.
	// We create it per Stop call.
}

func (rt *engineRuntime) register(c runtimeComponent) {
	if c == nil {
		return
	}
	p := rt.components
	// small optimization: avoid duplicates by pointer equality
	for _, existing := range p {
		if existing == c {
			return
		}
	}
	rt.components = append(rt.components, c)
}

func (rt *engineRuntime) stopAll() {
	for _, c := range rt.components {
		c.stop()
	}
}

func (rt *engineRuntime) waitAll(ctx context.Context) error {
	for _, c := range rt.components {
		if err := c.wait(ctx); err != nil {
			return err
		}
	}
	return nil
}
