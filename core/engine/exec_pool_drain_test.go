package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ExecPool.Drain 取消语义回归测试。
//
// 回归背景：Drain 先同步清空等待队列、再等待运行中的 goroutine，且清空过程
// 完全不看 ctx。队列里有阻塞任务时，Shutdown 会无视调用方给的超时被拖住。

// TestExecPool_DrainStopsOnContextCancel 验证 ctx 取消后 Drain 不再启动
// 队列中的新任务，而是立即返回取消错误。
func TestExecPool_DrainStopsOnContextCancel(t *testing.T) {
	pool := NewExecPool(ExecPoolConfig{MaxConcurrency: 1, QueueSize: 4})
	require.NotNil(t, pool)

	release := make(chan struct{})
	defer close(release)

	// 占用唯一令牌，让后续任务进入等待队列
	started := make(chan struct{})
	require.True(t, pool.TrySubmit(func() {
		close(started)
		<-release
	}))
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// 队列中的第一个任务在 ctx 取消后才返回
	require.True(t, pool.TrySubmit(func() { <-ctx.Done() }))
	// 第二个任务会一直阻塞：旧实现会在清空队列时把它拉起来同步执行
	require.True(t, pool.TrySubmit(func() { <-release }))

	done := make(chan error, 1)
	go func() { done <- pool.Drain(ctx) }()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatal("Drain 未在 ctx 取消后返回（等待队列中的任务阻塞了 Drain）")
	}
}
