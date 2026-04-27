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
	if c.e.internals.pendingDeleteStop != nil {
		c.e.internals.pendingDeleteStop()
		c.e.internals.pendingDeleteStop = nil
	}
	c.e.writeMu.Unlock()
}

func (c *pendingDeleteComponent) wait(ctx context.Context) error {
	// 等待 pending delete processor goroutine 退出。
	// 若 done 未设置（如测试中未初始化），则立即返回。
	done := c.e.internals.pendingDeleteDone
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
