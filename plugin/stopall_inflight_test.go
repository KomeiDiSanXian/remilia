package plugin

// stopall_inflight_test.go — StopAll 必须等待在飞重载的回归测试
//
// 回归背景：StopAll 的 tryTransition 失败即 continue（静默跳过）。插件处于
// Reloading/Loading 时会被直接跳过：停机后旧实例仍持有订阅与 Matcher，
// 在飞重载的收尾还会向已停机的 Manager 提交通知，而调用方看不到任何提示。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopAll_WaitsForInFlightReload(t *testing.T) {
	pm := NewManager(nil)

	var tornDown atomic.Bool
	require.NoError(t, pm.Register(&Descriptor{
		Name:  "inflight",
		Setup: func(*SetupContext) (any, error) { return nil, nil },
		Teardown: func(*TeardownContext) error {
			tornDown.Store(true)
			return nil
		},
	}))

	pm.mu.RLock()
	inst := pm.plugins["inflight"]
	pm.mu.RUnlock()
	require.NotNil(t, inst)

	// 模拟在飞重载
	inst.SetState(Reloading)

	done := make(chan error, 1)
	go func() { done <- pm.StopAll(context.Background()) }()

	select {
	case err := <-done:
		t.Fatalf("StopAll must wait for the in-flight reload instead of skipping it (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
		// 期望：等待中
	}

	// 重载完成 → StopAll 应继续并完成卸载
	inst.SetState(Loaded)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("StopAll did not finish after the reload settled")
	}

	assert.True(t, tornDown.Load(), "等待结束后必须完成卸载")
}

func TestStopAll_HonoursContextWhileWaiting(t *testing.T) {
	pm := NewManager(nil)
	require.NoError(t, pm.Register(&Descriptor{
		Name:  "stuck-reload",
		Setup: func(*SetupContext) (any, error) { return nil, nil },
	}))

	pm.mu.RLock()
	inst := pm.plugins["stuck-reload"]
	pm.mu.RUnlock()
	inst.SetState(Reloading)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	start := time.Now()
	// 在飞重载永不落定：StopAll 受 ctx 约束返回错误（停机不完整），而不是无限等待
	err := pm.StopAll(ctx)
	require.Error(t, err, "停机不完整必须报错而不是静默跳过")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 3*time.Second)
}
