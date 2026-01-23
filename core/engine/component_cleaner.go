package engine

import "context"

type tempCleanerComponent struct {
	e *Engine
}

func (c *tempCleanerComponent) stop() {
	if c == nil || c.e == nil {
		return
	}
	c.e.writeMu.Lock()
	if c.e.services.tempMatcherCleanerStop != nil {
		c.e.services.tempMatcherCleanerStop()
		c.e.services.tempMatcherCleanerStop = nil
	}
	c.e.services.tempMatcherCleanerDone = nil
	c.e.writeMu.Unlock()
}

func (c *tempCleanerComponent) wait(ctx context.Context) error {
	// current implementation has no separate done signal beyond cancellation; treat as immediate.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
