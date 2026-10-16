package plugin

// loading_guard_test.go — Loading（Setup 在飞）期间的注销/重载守卫回归测试
//
// 回归背景：实例在 Setup 开始前就以 Loading 入表、Setup 在锁外执行，而
// Unregister 只拦 Unloading、reload 只拦 Reloading/Unloading——Loading 全部
// 落穿。清理会先于 Setup 完成执行，随后 Setup 收尾把状态改成 Loaded、把 API
// 写回容器、把 Matcher 注册进引擎，留下无人管理的幽灵插件。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_UnregisterWaitsForInFlightSetup(t *testing.T) {
	pm := NewManager(nil)

	release := make(chan struct{})
	setupEntered := make(chan struct{})
	var setupFinished, teardownBeforeFinish atomic.Bool

	desc := &Descriptor{
		Name: "slow-load",
		Setup: func(ctx *SetupContext) (any, error) {
			close(setupEntered)
			<-release
			setupFinished.Store(true)
			return "slow-load-api", nil
		},
		Teardown: func(ctx *TeardownContext) error {
			if !setupFinished.Load() {
				teardownBeforeFinish.Store(true)
			}
			return nil
		},
	}

	regDone := make(chan error, 1)
	go func() { regDone <- pm.Register(desc) }()

	select {
	case <-setupEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("setup did not start")
	}

	unregDone := make(chan error, 1)
	go func() { unregDone <- pm.Unregister(context.Background(), "slow-load") }()

	// Setup 仍被阻塞：注销必须等待（旧实现在此处就会跑完 unload）
	select {
	case err := <-unregDone:
		t.Fatalf("Unregister must not finish while setup is in flight (returned: %v)", err)
	case <-time.After(80 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-regDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Register did not finish")
	}
	select {
	case err := <-unregDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Unregister did not finish")
	}

	assert.False(t, teardownBeforeFinish.Load(), "Teardown 不得先于 Setup 完成")
	assert.Equal(t, 0, pm.Count(), "注销后实例不应留在 plugins 表")
}

func TestInstance_ReloadWaitsForInFlightSetup(t *testing.T) {
	pm := NewManager(nil)

	release := make(chan struct{})
	setupEntered := make(chan struct{})
	var setupCalls atomic.Int32
	var setuppedCount atomic.Int32
	var teardownBeforeFinish atomic.Bool

	desc := &Descriptor{
		Name: "slow-reload",
		Setup: func(ctx *SetupContext) (any, error) {
			if setupCalls.Add(1) == 1 {
				close(setupEntered)
			}
			<-release
			setuppedCount.Add(1)
			return nil, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			// 首次 Setup 尚未结束时执行 Teardown 即旧实现的交叠
			if setuppedCount.Load() == 0 {
				teardownBeforeFinish.Store(true)
			}
			return nil
		},
	}

	regDone := make(chan error, 1)
	go func() { regDone <- pm.Register(desc) }()

	select {
	case <-setupEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("setup did not start")
	}

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- pm.Reload(context.Background(), "slow-reload") }()

	// Setup 仍被阻塞：重载必须等待（旧实现此处已开始 unload，Teardown 先跑）
	select {
	case err := <-reloadDone:
		t.Fatalf("reload must not start while setup is in flight (returned: %v)", err)
	case <-time.After(80 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-regDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Register did not finish")
	}
	select {
	case err := <-reloadDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("reload did not finish")
	}

	assert.False(t, teardownBeforeFinish.Load(), "Teardown 不得先于 Setup 完成")
	assert.Equal(t, int32(2), setupCalls.Load(), "等待结束后重载应正常执行（二次 Setup）")
}

func TestManager_UnregisterDuringLoadingHonoursContext(t *testing.T) {
	pm := NewManager(nil)

	release := make(chan struct{})
	setupEntered := make(chan struct{})

	desc := &Descriptor{
		Name: "hung-load",
		Setup: func(ctx *SetupContext) (any, error) {
			close(setupEntered)
			<-release
			return nil, nil
		},
	}

	regDone := make(chan error, 1)
	go func() { regDone <- pm.Register(desc) }()

	select {
	case <-setupEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("setup did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := pm.Unregister(ctx, "hung-load")
	require.Error(t, err, "等待超时应返回错误而不是与 Setup 交叠")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// 让在飞的 Setup 结束，回收 Register goroutine
	close(release)
	select {
	case err := <-regDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Register did not finish")
	}
}
