package remilia

import "context"

type pendingDeleteComponent struct {
	e *Engine
}

func (c *pendingDeleteComponent) stop() {
	if c == nil || c.e == nil {
		return
	}
	c.e.writeMu.Lock()
	if c.e.s.pendingDeleteStop != nil {
		c.e.s.pendingDeleteStop()
		c.e.s.pendingDeleteStop = nil
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
