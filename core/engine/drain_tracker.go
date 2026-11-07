package engine

import (
	"context"
	"sync"
)

// drainTracker 是一个可重复等待的「在途任务」计数器。
//
// 为什么不直接用 sync.WaitGroup：WaitGroup 在计数器归零后若又有 Add 进来
// （Submit 与 Drain/Shutdown 并发时必然出现），会命中 "WaitGroup is reused
// before previous Wait has returned" 的运行时检查，且 Add 与 Wait 并发本身
// 就是文档禁止的用法（Drain 可能在上一次 Wait 返回前就被再次调用）。
//
// 语义：
//   - Add 永远安全，可与 Wait 并发；
//   - Wait 在计数归零时返回；并发 Add 可能让本次 Wait 之后才执行的任务
//     不被计入（调用方需要精确排空时应先用关闭栅栏阻止新提交）；
//   - 等待者可被 ctx 取消。
type drainTracker struct {
	mu sync.Mutex
	n  int
	// zero 在 n>0 时为一个待关闭的 channel；n 归零时关闭并置 nil。
	zero chan struct{}
}

// Add 增加（delta>0）或减少（delta<0）在途任务计数。
func (t *drainTracker) Add(delta int) {
	if t == nil || delta == 0 {
		return
	}
	t.mu.Lock()
	if delta > 0 {
		if t.n == 0 {
			t.zero = make(chan struct{})
		}
		t.n += delta
	} else {
		t.n += delta
		if t.n <= 0 {
			t.n = 0
			if t.zero != nil {
				close(t.zero)
				t.zero = nil
			}
		}
	}
	t.mu.Unlock()
}

// Done 等价于 Add(-1)。
func (t *drainTracker) Done() { t.Add(-1) }

// Wait 阻塞到计数归零或 ctx 结束。
func (t *drainTracker) Wait(ctx context.Context) error {
	if t == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		t.mu.Lock()
		if t.n == 0 {
			t.mu.Unlock()
			return nil
		}
		zero := t.zero
		t.mu.Unlock()

		select {
		case <-zero:
			// 可能又有新任务加入，回到循环重新检查。
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Count 返回当前在途任务数（观测用）。
func (t *drainTracker) Count() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.n
}
