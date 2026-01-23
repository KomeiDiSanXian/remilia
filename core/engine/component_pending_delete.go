package engine

import "context"

type pendingDeleteComponent struct {
	e *Engine
}

func (c *pendingDeleteComponent) stop() {
	if c == nil || c.e == nil {
		return
	}
	c.e.writeMu.Lock()
	if c.e.services.pendingDeleteStop != nil {
		c.e.services.pendingDeleteStop()
		c.e.services.pendingDeleteStop = nil
	}
	c.e.writeMu.Unlock()
}

func (c *pendingDeleteComponent) wait(ctx context.Context) error {
	// pending delete processor currently exposes no done channel; treat as immediate.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
