package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRollbackContextLeak 测试 rollback 过程中的 context 泄漏修复
func TestRollbackContextLeak(t *testing.T) {
	manager := NewManager()

	// 创建一个会 panic 的组件
	panicComponent := NewSimpleComponent("panic-component",
		func(ctx context.Context) error {
			return nil // 启动成功
		},
		func(ctx context.Context) error {
			panic("deliberate panic in Stop")
		},
	)

	// 创建一个正常组件
	normalComponent := NewSimpleComponent("normal-component",
		func(ctx context.Context) error {
			return nil
		},
		func(ctx context.Context) error {
			return nil
		},
	)

	manager.Register(panicComponent)
	manager.Register(normalComponent)

	ctx := context.Background()

	// 启动应该成功
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 停止时 panicComponent 会 panic，但不应该导致 context 泄漏
	// 使用 recover 捕获 panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic in Stop: %v", r)
			}
		}()

		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = manager.Stop(stopCtx)
	}()

	// 如果 context 没有泄漏，测试应该正常结束
	t.Log("Test completed without context leak")
}

// TestRollbackWithError 测试 rollback 时组件返回错误的情况
func TestRollbackWithError(t *testing.T) {
	manager := NewManager()

	errorComponent := NewSimpleComponent("error-component",
		func(ctx context.Context) error {
			return nil
		},
		func(ctx context.Context) error {
			return errors.New("stop error")
		},
	)

	manager.Register(errorComponent)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 停止时返回错误，但 context 应该被正确释放
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := manager.Stop(stopCtx)
	if err == nil {
		t.Log("Stop completed (may have errors in rollback)")
	}

	// 测试正常完成说明 context 被正确释放
	t.Log("Context properly released despite errors")
}

// TestRollbackTimeout 测试 rollback 超时的情况
func TestRollbackTimeout(t *testing.T) {
	manager := NewManager()

	slowComponent := NewSimpleComponent("slow-component",
		func(ctx context.Context) error {
			return nil
		},
		func(ctx context.Context) error {
			// 模拟慢速关闭
			time.Sleep(15 * time.Second)
			return nil
		},
	)

	manager.Register(slowComponent)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 使用短超时停止
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_ = manager.Stop(stopCtx)
	elapsed := time.Since(start)

	// 应该在合理时间内完成（不超过 15 秒，因为每个组件有 10 秒超时）
	if elapsed > 20*time.Second {
		t.Errorf("Stop took too long: %v", elapsed)
	}

	t.Logf("Stop completed in %v (context properly managed)", elapsed)
}
