package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// testComponent is a test component
type testComponent struct {
	name         string
	startErr     error
	runErr       error
	stopErr      error
	startCalled  atomic.Bool
	runCalled    atomic.Bool
	stopCalled   atomic.Bool
	runCompleted chan struct{}
	blockRun     bool
}

func newTestComponent(name string) *testComponent {
	return &testComponent{
		name:         name,
		runCompleted: make(chan struct{}),
	}
}

func (c *testComponent) Name() string {
	return c.name
}

func (c *testComponent) OnStart(_ context.Context) error {
	c.startCalled.Store(true)
	return c.startErr
}

func (c *testComponent) OnRun(ctx context.Context) error {
	c.runCalled.Store(true)
	defer close(c.runCompleted)

	if c.blockRun {
		<-ctx.Done()
		return nil
	}

	return c.runErr
}

func (c *testComponent) OnStop(_ context.Context) error {
	c.stopCalled.Store(true)
	return c.stopErr
}

// TestManager_BasicLifecycle tests basic lifecycle
func TestManager_BasicLifecycle(t *testing.T) {
	manager := NewManager()

	comp1 := newTestComponent("comp1")
	comp1.blockRun = true
	comp2 := newTestComponent("comp2")
	comp2.blockRun = true

	manager.Register(comp1)
	manager.Register(comp2)

	// Start
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify state
	if manager.State() != StateRunning {
		t.Errorf("Expected state=running, got=%s", manager.State())
	}

	// Wait for OnRun goroutines to start, then verify
	for range 20 {
		if comp1.runCalled.Load() && comp2.runCalled.Load() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !comp1.startCalled.Load() || !comp1.runCalled.Load() {
		t.Error("comp1 not called")
	}
	if !comp2.startCalled.Load() || !comp2.runCalled.Load() {
		t.Error("comp2 not called")
	}

	// Stop
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify state
	if manager.State() != StateStopped {
		t.Errorf("Expected state=stopped, got=%s", manager.State())
	}

	// Verify OnStop was called
	if !comp1.stopCalled.Load() || !comp2.stopCalled.Load() {
		t.Error("OnStop not called")
	}
}

// TestManager_RunContextCancellation tests runtime context cancellation
func TestManager_RunContextCancellation(t *testing.T) {
	manager := NewManager()

	contextCancelled := make(chan struct{})
	comp := NewSimpleComponent(
		"test",
		nil,
		func(ctx context.Context) error {
			<-ctx.Done()
			close(contextCancelled)
			return nil
		},
		nil,
	)

	manager.Register(comp)

	// Start
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop
	stopCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify context was cancelled
	select {
	case <-contextCancelled:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("Context was not cancelled")
	}
}

// TestManager_StartError tests start error and rollback
func TestManager_StartError(t *testing.T) {
	manager := NewManager()

	comp1 := newTestComponent("comp1")
	comp2 := newTestComponent("comp2")
	comp2.startErr = errors.New("start failed")
	comp3 := newTestComponent("comp3")

	manager.Register(comp1)
	manager.Register(comp2)
	manager.Register(comp3)

	// Start should fail
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := manager.Start(ctx)
	if err == nil {
		t.Fatal("Expected start error")
	}

	// Verify state
	if manager.State() != StateCreated {
		t.Errorf("Expected state=created after failure, got=%s", manager.State())
	}

	// Verify comp1 OnStop was called (rollback is synchronous in Start)
	if !comp1.stopCalled.Load() {
		t.Error("comp1 OnStop not called during rollback")
	}

	// Verify comp3 was not started
	if comp3.startCalled.Load() {
		t.Error("comp3 should not have been started")
	}
}

// TestManager_StopError tests stop error
func TestManager_StopError(t *testing.T) {
	manager := NewManager()

	comp1 := newTestComponent("comp1")
	comp1.blockRun = true
	comp1.stopErr = errors.New("stop failed")
	comp2 := newTestComponent("comp2")
	comp2.blockRun = true

	manager.Register(comp1)
	manager.Register(comp2)

	// Start
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop (will have error, but should not panic)
	stopCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := manager.Stop(stopCtx)

	// Should return error
	if err == nil {
		t.Error("Expected stop error")
	}

	// But state should be stopped
	if manager.State() != StateStopped {
		t.Errorf("Expected state=stopped, got=%s", manager.State())
	}
}

// TestManager_MultipleComponents tests multiple components
func TestManager_MultipleComponents(t *testing.T) {
	manager := NewManager()

	const numComponents = 5
	comps := make([]*testComponent, numComponents)

	for i := range numComponents {
		comp := newTestComponent(string(rune('A' + i)))
		comp.blockRun = true
		comps[i] = comp
		manager.Register(comp)
	}

	// Start
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for OnRun goroutines to start, then verify
	for range 20 {
		allStarted := true
		for _, comp := range comps {
			if !comp.runCalled.Load() {
				allStarted = false
				break
			}
		}
		if allStarted {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i, comp := range comps {
		if !comp.startCalled.Load() {
			t.Errorf("Component %d OnStart not called", i)
		}
		if !comp.runCalled.Load() {
			t.Errorf("Component %d OnRun not called", i)
		}
	}

	// Stop
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify all components OnStop were called
	for i, comp := range comps {
		if !comp.stopCalled.Load() {
			t.Errorf("Component %d OnStop not called", i)
		}
	}
}

// TestSimpleComponent tests SimpleComponent
func TestSimpleComponent(t *testing.T) {
	var startCalled atomic.Bool
	var runCalled atomic.Bool
	var stopCalled atomic.Bool

	comp := NewSimpleComponent(
		"test",
		func(ctx context.Context) error {
			startCalled.Store(true)
			return nil
		},
		func(ctx context.Context) error {
			runCalled.Store(true)
			<-ctx.Done()
			return nil
		},
		func(ctx context.Context) error {
			stopCalled.Store(true)
			return nil
		},
	)

	manager := NewManager()
	manager.Register(comp)

	// Start
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	for range 20 {
		if runCalled.Load() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !startCalled.Load() || !runCalled.Load() {
		t.Error("Start or Run not called")
	}

	// Stop
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if !stopCalled.Load() {
		t.Error("Stop not called")
	}
}

// TestResourceComponent tests ResourceComponent
func TestResourceComponent(t *testing.T) {
	var resource string
	acquireCalled := false
	releaseCalled := false

	comp := NewResourceComponent(
		"test",
		func(ctx context.Context) (any, error) {
			acquireCalled = true
			return "test-resource", nil
		},
		func(ctx context.Context, res any) error {
			releaseCalled = true
			resource = res.(string)
			return nil
		},
	)

	manager := NewManager()
	manager.Register(comp)

	// Start
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !acquireCalled {
		t.Error("Acquire not called")
	}

	// Stop
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if !releaseCalled {
		t.Error("Release not called")
	}

	if resource != "test-resource" {
		t.Errorf("Resource mismatch: got=%s", resource)
	}
}

// TestManager_ComponentStatuses 测试组件健康状态聚合
func TestManager_ComponentStatuses(t *testing.T) {
	manager := NewManager()

	comp1 := newTestComponent("comp1")
	comp1.blockRun = true
	comp2 := newTestComponent("comp2")
	comp2.blockRun = false
	comp2.runErr = errors.New("comp2 failed")

	manager.Register(comp1)
	manager.Register(comp2)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// comp2 会快速退出（runErr 非空），等待状态更新
	for range 20 {
		statuses := manager.ComponentStatuses()
		if st, ok := statuses["comp2"]; ok && !st.Running {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	statuses := manager.ComponentStatuses()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// comp1 应仍在运行
	if st, ok := statuses["comp1"]; !ok || !st.Running {
		t.Error("comp1 should still be running")
	}

	// comp2 应已退出且有错误
	if st, ok := statuses["comp2"]; !ok || st.Running {
		t.Error("comp2 should have exited")
	} else if st.ExitErr == nil {
		t.Error("comp2 should have ExitErr")
	}

	// HasUnhealthyComponents 应返回 true
	if names, unhealthy := manager.HasUnhealthyComponents(); !unhealthy {
		t.Error("should have unhealthy components, got names:", names)
	}

	// 正常停止
	_ = manager.Stop(context.Background())
}

// TestManager_ComponentStatuses_AllHealthy 测试所有组件健康时的状态
func TestManager_ComponentStatuses_AllHealthy(t *testing.T) {
	manager := NewManager()

	comp := newTestComponent("comp")
	comp.blockRun = true
	manager.Register(comp)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	statuses := manager.ComponentStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	if st := statuses["comp"]; !st.Running {
		t.Error("comp should be running")
	}

	if names, unhealthy := manager.HasUnhealthyComponents(); unhealthy {
		t.Error("should not have unhealthy components, got names:", names)
	}

	_ = manager.Stop(context.Background())
}
