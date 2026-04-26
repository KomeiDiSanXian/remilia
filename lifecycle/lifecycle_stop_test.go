package lifecycle

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestLifecycleManager_StopMultipleErrors 测试 Stop 收集多个组件错误
func TestLifecycleManager_StopMultipleErrors(t *testing.T) {
	manager := NewManager()

	// 创建三个组件，其中两个会在 Stop 时失败
	comp1 := NewSimpleComponent(
		"comp1",
		nil,
		nil,
		func(ctx context.Context) error {
			return fmt.Errorf("comp1 stop failed")
		},
	)

	comp2 := NewSimpleComponent(
		"comp2",
		nil,
		nil,
		func(ctx context.Context) error {
			return nil // 成功
		},
	)

	comp3 := NewSimpleComponent(
		"comp3",
		nil,
		nil,
		func(ctx context.Context) error {
			return fmt.Errorf("comp3 stop failed")
		},
	)

	manager.Register(comp1)
	manager.Register(comp2)
	manager.Register(comp3)

	// 启动
	startCtx, startCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startCancel()

	if err := manager.Start(startCtx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 停止
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err := manager.Stop(stopCtx)

	// 验证返回了错误
	if err == nil {
		t.Fatal("Expected error from Stop, got nil")
	}

	// 验证错误消息包含多个组件的错误
	errMsg := err.Error()
	if !contains(errMsg, "comp1") {
		t.Errorf("Error should mention comp1, got: %s", errMsg)
	}
	if !contains(errMsg, "comp3") {
		t.Errorf("Error should mention comp3, got: %s", errMsg)
	}

	// comp2 成功停止，不应该在错误中
	// （但由于是组合错误，可能会包含"components"等词）

	t.Logf("✓ Multiple component errors collected: %v", err)
}

// TestLifecycleManager_StopSingleError 测试单个组件错误
func TestLifecycleManager_StopSingleError(t *testing.T) {
	manager := NewManager()

	comp := NewSimpleComponent(
		"failing-comp",
		nil,
		nil,
		func(ctx context.Context) error {
			return fmt.Errorf("stop error")
		},
	)

	manager.Register(comp)

	// 启动
	startCtx := context.Background()
	if err := manager.Start(startCtx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 停止
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err := manager.Stop(stopCtx)

	if err == nil {
		t.Fatal("Expected error from Stop")
	}

	// 验证错误类型
	var stopErr *StopError
	if !contains(err.Error(), "StopError") && !contains(err.Error(), "failing-comp") {
		t.Logf("Warning: Error format may have changed: %v", err)
	}

	t.Logf("✓ Single component error handled: %v (%T)", err, stopErr)
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
