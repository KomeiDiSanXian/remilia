package engine

import (
	stdctx "context"
	"sync/atomic"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ProcessEventSync 防护层回归测试。
//
// 回归背景：ProcessEventSync 直接调用 processEventMatchers，绕过了
// shutdown 检查、eventWg 计数与 panic 恢复。结果是 Shutdown 返回后同步入口
// 仍会执行 handler，规则/中间件构造中的 panic 会直接抛给调用方。

// TestProcessEventSync_RespectsShutdown 验证 Shutdown 之后同步入口不再处理事件。
func TestProcessEventSync_RespectsShutdown(t *testing.T) {
	eng := newEngineForTest(t)

	var handled atomic.Int32
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(*ctx.Context) error {
		handled.Add(1)
		return nil
	})

	require.NoError(t, eng.Shutdown(stdctx.Background()))

	eng.ProcessEventSync(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	assert.Equal(t, int32(0), handled.Load(), "Shutdown 之后同步入口不应再执行 handler")
}

// TestProcessEventSync_RecoversPanic 验证同步入口有 panic 兜底：
// 规则中的 panic 不应抛给调用方（调用方是插件或平台适配器）。
func TestProcessEventSync_RecoversPanic(t *testing.T) {
	eng := newEngineForTest(t)

	eng.On(string(platform.EventKindPrivateMessage)).
		Where(func(*ctx.Context) bool { panic("boom in rule") }).
		Handle(func(*ctx.Context) error { return nil })

	assert.NotPanics(t, func() {
		eng.ProcessEventSync(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	})
}

// TestProcessEventSync_TrackedByShutdownWait 验证进行中的同步事件会被
// Shutdown 等待：Shutdown 必须在 handler 结束之后才返回。
func TestProcessEventSync_TrackedByShutdownWait(t *testing.T) {
	eng := newEngineForTest(t)

	started := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool

	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(*ctx.Context) error {
		close(started)
		<-release
		finished.Store(true)
		return nil
	})

	go eng.ProcessEventSync(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	<-started

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- eng.Shutdown(stdctx.Background()) }()

	select {
	case <-shutdownDone:
		t.Fatal("Shutdown 不应在同步 handler 结束前返回")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-shutdownDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown 未在同步 handler 结束后返回")
	}
	assert.True(t, finished.Load())
}
