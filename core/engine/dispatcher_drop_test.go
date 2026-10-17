package engine

// dispatcher_drop_test.go — 队列被放弃时任务必须得到结果的回归测试
//
// 回归背景：worker panic 后重建队列、worker 因停机上下文结束而退出，
// 这两种情况都会丢弃队列里尚未执行的任务。此前这些任务连同各自的
// Future 被静默丢弃：任务永不执行、调用方 Wait 永久等待
// （文档推荐 `future.Wait(ctx.Context())`，而该 context 通常没有 deadline）。

import (
	stdctx "context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/future"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher_PanicDropResolvesQueuedTasks(t *testing.T) {
	d := NewOutboundDispatcher(stdctx.Background(), DispatcherConfig{
		MaxInflight: 1,
		QueueSize:   4,
	})
	defer d.ForceClose()

	entered := make(chan struct{})
	release := make(chan struct{})

	// 占住 worker，然后 panic：队列会被重建，后面的任务落入"被放弃"集合
	require.NoError(t, d.Submit("chat-panic", func(stdctx.Context) error {
		close(entered)
		<-release
		panic("boom")
	}))

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first task did not start")
	}

	var droppedRan atomic.Bool
	f := future.New[string]()
	require.NoError(t, d.Submit("chat-panic", func(ctx stdctx.Context) error {
		droppedRan.Store(true)
		err := ctx.Err()
		if err == nil {
			err = errors.New("unexpectedly executed with a live context")
		}
		f.Resolve("", err)
		return ctx.Err()
	}))

	close(release) // 触发 worker panic 与队列重建

	// 被放弃的任务必须被唤醒（用已取消的 context 快速失败），而不是消失
	require.Eventually(t, droppedRan.Load, 2*time.Second, 5*time.Millisecond,
		"queued task must be woken up instead of being dropped silently")

	val, err := f.Wait(mustTimeoutCtx(t, time.Second))
	assert.Empty(t, val)
	require.Error(t, err, "被放弃的任务应把取消错误写入 Future")
	assert.ErrorIs(t, err, stdctx.Canceled)
}

func TestDispatcher_StopDropResolvesQueuedTasks(t *testing.T) {
	d := NewOutboundDispatcher(stdctx.Background(), DispatcherConfig{
		MaxInflight: 1, // 只有一个并发令牌：第二个 chat 的 worker 会阻塞在获取令牌上
		QueueSize:   4,
	})

	blocking := make(chan struct{})
	releaseSend := make(chan struct{})

	// chat A 的任务占住唯一令牌并阻塞在发送中
	require.NoError(t, d.Submit("chat-a", func(ctx stdctx.Context) error {
		close(blocking)
		select {
		case <-releaseSend:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))

	select {
	case <-blocking:
	case <-time.After(2 * time.Second):
		t.Fatal("first task did not start")
	}

	// chat B 的 worker 在获取令牌前被停机上下文取消 → 走放弃分支
	var droppedRan atomic.Bool
	f := future.New[string]()
	require.NoError(t, d.Submit("chat-b", func(ctx stdctx.Context) error {
		droppedRan.Store(true)
		err := ctx.Err()
		if err == nil {
			err = errors.New("unexpectedly executed with a live context")
		}
		f.Resolve("", err)
		return ctx.Err()
	}))

	time.Sleep(50 * time.Millisecond) // 让 chat B 的 worker 进入等待令牌
	d.ForceClose()
	close(releaseSend)

	require.Eventually(t, droppedRan.Load, 2*time.Second, 5*time.Millisecond,
		"停机时队列中的任务必须被唤醒")

	_, err := f.Wait(mustTimeoutCtx(t, time.Second))
	require.Error(t, err)
	assert.ErrorIs(t, err, stdctx.Canceled)
}

func mustTimeoutCtx(t *testing.T, d time.Duration) stdctx.Context {
	t.Helper()
	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
