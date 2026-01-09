package middleware

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestTimeoutWithContextPropagation 测试超时中间件是否正确传递 context
func TestTimeoutWithContextPropagation(t *testing.T) {
	// 创建测试 payload
	payload := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-timeout-context",
	}
	ctx := remilia.NewContext(payload, nil)

	// 记录 context 是否有超时
	var contextHasDeadline bool
	var deadline time.Time

	// 创建一个 handler 来检查 context
	handler := func(ctx *remilia.Context) error {
		stdCtx := ctx.Context()
		deadline, contextHasDeadline = stdCtx.Deadline()
		return nil
	}

	// 应用超时中间件
	timeout := 5 * time.Second
	wrappedHandler := Timeout(timeout)(func(ctx *remilia.Context) error {
		return handler(ctx)
	})

	// 执行 handler
	if err := wrappedHandler(ctx); err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	// 验证 context 有 deadline
	if !contextHasDeadline {
		t.Error("Expected context to have deadline, but it doesn't")
	}

	// 验证 deadline 大约是在 5 秒后
	expectedDeadline := time.Now().Add(timeout)
	if deadline.Before(expectedDeadline.Add(-1*time.Second)) || deadline.After(expectedDeadline.Add(1*time.Second)) {
		t.Errorf("Expected deadline around %v, got %v", expectedDeadline, deadline)
	}
}

// TestTimeoutWithContextCancellation 测试超时时 context 是否被取消
func TestTimeoutWithContextCancellation(t *testing.T) {
	// 创建测试 payload
	payload := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-timeout-cancel",
	}
	ctx := remilia.NewContext(payload, nil)

	// 记录 context 是否被取消（避免 data race）
	var canceled atomic.Bool
	cerrCh := make(chan error, 1)

	// 创建一个长时间运行的 handler
	handler := func(ctx *remilia.Context) error {
		stdCtx := ctx.Context()

		// 模拟长时间操作，检查 context 取消
		select {
		case <-time.After(2 * time.Second):
			// 操作完成（不应该到达这里）
			return nil
		case <-stdCtx.Done():
			// Context 被取消
			canceled.Store(true)
			select {
			case cerrCh <- stdCtx.Err():
			default:
			}
			return stdCtx.Err()
		}
	}

	// 应用超时中间件（500ms 超时）
	timeout := 500 * time.Millisecond
	wrappedHandler := Timeout(timeout)(func(ctx *remilia.Context) error {
		return handler(ctx)
	})

	// 执行 handler（应该超时）
	err := wrappedHandler(ctx)
	if err == nil {
		t.Fatal("Expected handler to timeout, but it didn't")
	}

	// 给 goroutine 一点时间来检测取消
	time.Sleep(100 * time.Millisecond)

	// 验证 context 被取消
	if !canceled.Load() {
		t.Error("Expected context to be canceled on timeout")
	}

	var contextError error
	select {
	case contextError = <-cerrCh:
	default:
	}

	// 验证 context 错误是 DeadlineExceeded 或 Canceled
	if contextError != nil && !(errors.Is(contextError, context.DeadlineExceeded) || errors.Is(contextError, context.Canceled)) {
		t.Errorf("Expected context to be canceled (deadline exceeded or canceled), got: %v", contextError)
	}
}

// TestTimeoutWithContextRestoration 测试超时后 context 是否被恢复
func TestTimeoutWithContextRestoration(t *testing.T) {
	// 创建测试 payload
	payload := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-timeout-restore",
	}
	ctx := remilia.NewContext(payload, nil)

	// 记录原始 context
	originalCtx := ctx.Context()

	// 创建一个快速完成的 handler
	handler := func(ctx *remilia.Context) error {
		return nil
	}

	// 应用超时中间件
	timeout := 5 * time.Second
	wrappedHandler := Timeout(timeout)(func(ctx *remilia.Context) error {
		return handler(ctx)
	})

	// 执行 handler
	if err := wrappedHandler(ctx); err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	// 验证 context 被恢复为原始值
	restoredCtx := ctx.Context()
	if restoredCtx != originalCtx {
		t.Error("Expected context to be restored to original after handler completion")
	}
}

// TestTimeoutWithContextNested 测试嵌套超时中间件
func TestTimeoutWithContextNested(t *testing.T) {
	// 创建测试 payload
	payload := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-timeout-nested",
	}
	ctx := remilia.NewContext(payload, nil)

	// 记录两次的 deadline
	var outerDeadline, innerDeadline time.Time
	var outerHasDeadline, innerHasDeadline bool

	// 创建一个 handler
	handler := func(ctx *remilia.Context) error {
		stdCtx := ctx.Context()
		innerDeadline, innerHasDeadline = stdCtx.Deadline()
		return nil
	}

	// 应用两层超时中间件
	outerTimeout := 10 * time.Second
	innerTimeout := 5 * time.Second

	wrappedHandler := Timeout(outerTimeout)(func(ctx *remilia.Context) error {
		stdCtx := ctx.Context()
		outerDeadline, outerHasDeadline = stdCtx.Deadline()

		// 应用内层超时
		innerWrapped := Timeout(innerTimeout)(func(ctx *remilia.Context) error {
			return handler(ctx)
		})
		return innerWrapped(ctx)
	})

	// 执行 handler
	if err := wrappedHandler(ctx); err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	// 验证两个 context 都有 deadline
	if !outerHasDeadline {
		t.Error("Expected outer context to have deadline")
	}
	if !innerHasDeadline {
		t.Error("Expected inner context to have deadline")
	}

	// 验证内层 deadline 更早（因为超时更短）
	if !innerDeadline.Before(outerDeadline) {
		t.Errorf("Expected inner deadline (%v) to be before outer deadline (%v)",
			innerDeadline, outerDeadline)
	}
}
