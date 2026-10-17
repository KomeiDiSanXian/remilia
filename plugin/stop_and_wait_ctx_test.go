package plugin

// stop_and_wait_ctx_test.go — goroutine 停止等待必须受 ctx 约束的回归测试
//
// 回归背景：goroutineManager.stopAndWait 用 wg.Wait() 无上限等待。unload /
// StopAll / Shutdown / 蓝绿清理都在停机路径上调用它，插件 goroutine 不响应
// 取消（或在长操作中）时会把这些路径永久挂住，与"停机受 ctx 超时约束"的
// 文档契约矛盾。

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoroutineManager_StopAndWaitCtxTimesOut(t *testing.T) {
	gm := newGoroutineManagerForPlugin("blocked-plugin")

	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	// 不响应 ctx.Done() 的 goroutine：模拟插件后台任务卡在长操作
	gm.goNamed_("stuck", func(ctx context.Context) {
		close(entered)
		<-release
	})

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := gm.stopAndWaitCtx(ctx)
	require.Error(t, err, "应返回 ctx 错误而不是无限等待")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second, "超时应立即返回")
}

func TestGoroutineManager_StopAndWaitCtxCompletes(t *testing.T) {
	gm := newGoroutineManagerForPlugin("cooperative-plugin")

	done := make(chan struct{})
	gm.goNamed_("cooperative", func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, gm.stopAndWaitCtx(ctx))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine should have observed cancellation")
	}
}

func TestInstance_UnloadTimesOutOnStuckGoroutine(t *testing.T) {
	inst := &Instance{desc: &Descriptor{Name: "stuck-unload"}}

	gm := newGoroutineManagerForPlugin("stuck-unload")
	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	gm.goNamed_("stuck", func(ctx context.Context) {
		close(entered)
		<-release
	})

	inst.mu.Lock()
	inst.state = Loaded
	inst.goroutineMgr = gm
	inst.mu.Unlock()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := inst.unload(ctx, nil)
	require.Error(t, err, "卸载应因 ctx 到期返回错误，而不是永久阻塞")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, Error, inst.GetState())
}
