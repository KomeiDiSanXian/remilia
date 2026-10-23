package plugin

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestUnregisterRejectsReloading 验证注销必须拒绝正在重载的实例，
// 否则 Reload 的卸载/重建会与 Unregister 的清理交错。
func TestUnregisterRejectsReloading(t *testing.T) {
	pm := NewManager(nil)
	inst := &Instance{desc: &Descriptor{Name: "reloading-plugin"}, state: Reloading, manager: pm}
	pm.plugins["reloading-plugin"] = inst

	err := pm.Unregister(context.Background(), "reloading-plugin")
	if err == nil {
		t.Fatal("Unregister must reject a reloading instance")
	}
	if !strings.Contains(err.Error(), "reloading") {
		t.Errorf("error should mention reloading, got %v", err)
	}
	if got := inst.GetState(); got != Reloading {
		t.Errorf("state must stay Reloading, got %v", got)
	}
}

// TestScopeDisposeRetriesAfterCancel 验证 Dispose 因 ctx 取消提前返回后，
// 下一次调用必须继续执行剩余清理，而不是因为 disposed 已置位而永久跳过。
func TestScopeDisposeRetriesAfterCancel(t *testing.T) {
	s := &Scope{name: "retry"}
	var calls atomic.Int32
	s.OnDispose(func() error {
		calls.Add(1)
		return nil
	})

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Dispose(canceled); err == nil {
		t.Fatal("expected ctx cancellation error")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("hook must not run during canceled dispose, got %d calls", got)
	}

	if err := s.Dispose(context.Background()); err != nil {
		t.Fatalf("retry dispose failed: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("hook must run exactly once after retry, got %d", got)
	}

	// 已完成后再调用是 no-op，不重复执行。
	if err := s.Dispose(context.Background()); err != nil {
		t.Fatalf("second dispose failed: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("completed dispose must not rerun hooks, got %d", got)
	}
}

// TestEventBusWaitForInflight 验证退订后不再启动新 handler，
// Wait 会等待已启动的 handler 结束（Scope.Dispose 用它保证 Teardown 前无旧 handler）。
func TestEventBusWaitForInflight(t *testing.T) {
	eb := NewEventBus()

	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32

	sub, err := eb.Subscribe("topic", func(context.Context, any) error {
		if runs.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := eb.Publish("topic", nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	<-started

	if err := sub.Unsubscribe(); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	// 退订后发布：不应启动新的 handler（worker 槽位会立即归还）。
	if err := eb.Publish("topic", nil); err != nil {
		t.Fatalf("Publish after unsubscribe: %v", err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("no new handler may start after unsubscribe, got %d runs", got)
	}

	waiter, ok := sub.(interface{ Wait() })
	if !ok {
		t.Fatal("subscription must expose Wait")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		waiter.Wait()
	}()
	select {
	case <-done:
		t.Fatal("Wait returned while the handler was still running")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after the handler finished")
	}
}

// TestCallTeardownRecoversPanic 验证 Teardown panic 必须转成错误而不是崩进程。
func TestCallTeardownRecoversPanic(t *testing.T) {
	d := &Descriptor{
		Name: "boom",
		Teardown: func(*TeardownContext) error {
			panic("kaboom")
		},
	}
	err := d.callTeardown(&TeardownContext{})
	if err == nil {
		t.Fatal("expected an error from a panicking Teardown")
	}
	if !strings.Contains(err.Error(), "panic in Teardown") {
		t.Errorf("error should describe the panic, got %v", err)
	}
}

// TestPluginLoadFailureMetrics 验证加载失败按原因计数（setup / panic）。
func TestPluginLoadFailureMetrics(t *testing.T) {
	setupName := "metric-setup-failure"
	panicName := "metric-panic-failure"

	beforeSetup := testutil.ToFloat64(loadFailures.WithLabelValues(setupName, "setup"))
	beforePanic := testutil.ToFloat64(loadFailures.WithLabelValues(panicName, "panic"))

	pm := NewManager(nil)
	_ = pm.Register(&Descriptor{
		Name: setupName,
		Setup: func(*SetupContext) (any, error) {
			return nil, errors.New("setup failed")
		},
	})
	_ = pm.Register(&Descriptor{
		Name: panicName,
		Setup: func(*SetupContext) (any, error) {
			panic("boom")
		},
	})

	if got := testutil.ToFloat64(loadFailures.WithLabelValues(setupName, "setup")); got != beforeSetup+1 {
		t.Errorf("setup failure counter: got %v want %v", got, beforeSetup+1)
	}
	if got := testutil.ToFloat64(loadFailures.WithLabelValues(panicName, "panic")); got != beforePanic+1 {
		t.Errorf("panic failure counter: got %v want %v", got, beforePanic+1)
	}
}
