package fsm

// session_lifecycle_hardening_test.go — 会话生命周期边界的回归测试
//
// 回归背景：
//   - Engine.EndSession 不取会话锁：与并发 TryTransition 的读-改-写交错时，
//     删除后可能被旧快照 Save 回来（会话"复活"）；
//   - TryStartSession 先落库再执行回调：回调 panic 会绕过补偿删除，留下
//     半成品会话（回调返回错误时也依赖补偿）；
//   - StartCleanup 重复调用会叠加清理 goroutine，且清理无任何日志。

import (
	"context"
	"sync"
	"testing"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndSession_ConcurrentWithTransition(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:    "lifecycle",
		Initial: "idle",
		Events: []Event{
			{Name: "start", From: "idle", To: "waiting", Match: func(*corectx.Context) bool { return true }},
			{Name: "next", From: "waiting", To: "done", Match: func(*corectx.Context) bool { return true }},
		},
	}))

	for i := 0; i < 50; i++ {
		sessionID := "chat-lifecycle-concurrent"
		eng.EndSession(sessionID) // 清理上一轮
		ok, err := eng.TryStartSession(newTestContext(""), sessionID)
		require.NoError(t, err)
		require.True(t, ok)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _, _ = eng.TryTransition(newTestContext(""), sessionID)
		}()
		go func() {
			defer wg.Done()
			// 与迁移并发终止：结束后会话必须真正消失（不得被旧快照写回）
			eng.EndSession(sessionID)
			eng.EndSession(sessionID)
		}()
		wg.Wait()

		// 再次显式终止后断言会话不存在（两个 goroutine 都已结束，无并发）
		eng.EndSession(sessionID)
		assert.Nil(t, eng.GetSession(sessionID), "终止后的会话不得复活")
	}
}

func TestTryStartSession_FailedCallbackLeavesNoSession(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:    "failing-start",
		Initial: "idle",
		Events: []Event{
			{
				Name: "start", From: "idle", To: "waiting",
				Match: func(*corectx.Context) bool { return true },
				Action: func(*FSMContext) error {
					return assert.AnError
				},
			},
		},
	}))

	ok, err := eng.TryStartSession(newTestContext(""), "chat-failing-start")
	require.Error(t, err)
	assert.False(t, ok)
	assert.Nil(t, eng.GetSession("chat-failing-start"), "启动失败不得留下半成品会话")
}

func TestTryStartSession_PanickingCallbackLeavesNoSession(t *testing.T) {
	eng := NewEngine(nil)
	require.NoError(t, eng.Register(&FSM{
		Name:    "panicking-start",
		Initial: "idle",
		Events: []Event{
			{
				Name: "start", From: "idle", To: "waiting",
				Match: func(*corectx.Context) bool { return true },
				Action: func(*FSMContext) error {
					panic("boom")
				},
			},
		},
	}))

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("回调 panic 应向上传播（由调用方决定是否恢复）")
			}
		}()
		_, _ = eng.TryStartSession(newTestContext(""), "chat-panicking-start")
	}()

	assert.Nil(t, eng.GetSession("chat-panicking-start"),
		"panic 不得留下半成品会话（先回调后落库）")
}

func TestStartCleanup_IsIdempotentAndStoppable(t *testing.T) {
	store := NewMemoryStorage()
	eng := NewEngine(store)

	// 造一个已过期会话
	store.Save(&Session{
		ID: "expired", FSMName: "x", Current: "s",
		Data: map[string]any{}, ExpireAt: time.Now().Add(-time.Hour).Unix(),
	})

	stop := make(chan struct{})
	eng.StartCleanup(20*time.Millisecond, stop)
	eng.StartCleanup(20*time.Millisecond, stop) // 重复调用不得叠加

	require.Eventually(t, func() bool { return eng.GetSession("expired") == nil },
		2*time.Second, 10*time.Millisecond, "过期会话应被清理")

	close(stop)
	// 停止生效后允许重新启动
	require.Eventually(t, func() bool { return !eng.cleanupStarted.Load() },
		2*time.Second, 5*time.Millisecond, "停止通道关闭后清理 goroutine 应退出")
	eng.StartCleanup(20*time.Millisecond, make(chan struct{}))
	assert.True(t, eng.cleanupStarted.Load(), "停止后应可重新启动清理")
	_ = context.Background()
}
