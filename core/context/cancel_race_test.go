package context

// cancel_race_test.go — Cancel 并发安全与克隆 context 释放的回归测试
//
// 回归背景：ctx.cancel 是普通字段，Cancel() 裸读裸写（读判空 → 调用 → 置 nil）。
// 并发调用（池任务收尾与调用方主动取消重叠）构成数据竞争，且可能漏掉一次
// 取消调用。Clone 出来的 std context 也从未被释放。

import (
	stdctx "context"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContext_CancelConcurrentIsRaceFree(t *testing.T) {
	c := newTestCtx()
	cloned := c.Clone()
	require.NotNil(t, cloned.Context())

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cloned.Cancel()
		}()
	}
	wg.Wait()

	select {
	case <-cloned.Context().Done():
		// 期望：无论哪个 goroutine 胜出，克隆的 std context 都已取消
	case <-time.After(time.Second):
		t.Fatal("Cancel 后克隆的 std context 应已取消")
	}
}

func TestContext_CancelKeepsDeadline(t *testing.T) {
	// 带 deadline 的原 Context：克隆应保留同一 deadline 且可被主动取消
	deadline := time.Now().Add(time.Hour)
	parent, cancelParent := stdctx.WithDeadline(stdctx.Background(), deadline)
	defer cancelParent()

	c := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	c.SetStdContext(parent)

	cloned := c.Clone()
	got, ok := cloned.Context().Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, deadline, got, time.Second)

	cloned.Cancel()
	select {
	case <-cloned.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("Cancel 应生效")
	}
}
