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
	c.e.writeMu.Unlock()
}

func (c *tempCleanerComponent) wait(ctx context.Context) error {
	if c == nil || c.e == nil {
		return nil
	}
	// 修复 #11：等待 tempMatcherCleanerDone channel 关闭，确认后台 goroutine 真正退出。
	// 原实现直接返回 nil，导致 Engine.Shutdown() 可能在 cleaner goroutine 退出前返回，
	// 造成 goroutine 泄漏（goleak 可检出）。
	c.e.writeMu.Lock()
	done := c.e.services.tempMatcherCleanerDone
	c.e.writeMu.Unlock()

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
