package context

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// TestContextClone_IndependentCancellation 测试克隆的 Context 不受原 Context 取消影响
func TestContextClone_IndependentCancellation(t *testing.T) {
	// 创建原始 context，带取消功能
	stdCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	payload := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-123",
	}

	originalCtx := NewContextWithContext(stdCtx, payload, nil)

	// 克隆 context
	clonedCtx := originalCtx.Clone()

	// 取消原始 context
	cancel()

	// 等待一小段时间确保取消信号传播
	time.Sleep(10 * time.Millisecond)

	// 验证原始 context 已取消
	select {
	case <-originalCtx.Context().Done():
		// 期望行为：原始 context 已取消
	default:
		t.Error("Original context should be canceled")
	}

	// 验证克隆的 context 未受影响
	select {
	case <-clonedCtx.Context().Done():
		t.Error("Cloned context should NOT be canceled when original is canceled")
	default:
		// 期望行为：克隆的 context 仍然有效
	}

	t.Log("✓ Cloned context is independent from original context cancellation")
}

// TestContextClone_TracePreservation 测试克隆保留 trace 信息
// 注意：此测试在 OpenTelemetry 未配置时会跳过
func TestContextClone_TracePreservation(t *testing.T) {
	// 创建带 trace 的 context
	tracer := otel.Tracer("test-tracer")
	stdCtx, span := tracer.Start(context.Background(), "test-operation")
	defer span.End()

	// 检查 span 是否有效（默认是 NoOp tracer）
	if !span.SpanContext().IsValid() {
		t.Skip("Skipping: OpenTelemetry not configured, using NoOp tracer")
		return
	}

	// 如果 span 有效，继续测试
	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	originalCtx := NewContextWithContext(stdCtx, payload, nil)
	clonedCtx := originalCtx.Clone()

	originalSpan := trace.SpanFromContext(originalCtx.Context())
	clonedSpan := trace.SpanFromContext(clonedCtx.Context())

	if !originalSpan.SpanContext().IsValid() {
		t.Error("Original context should have valid span")
	}

	if !clonedSpan.SpanContext().IsValid() {
		t.Error("Cloned context should have valid span")
	}

	if originalSpan.SpanContext().TraceID() != clonedSpan.SpanContext().TraceID() {
		t.Error("Cloned context should preserve trace ID")
	}

	t.Log("✓ Trace information preserved in cloned context")
}

// TestContextClone_EventCopied 测试事件被正确克隆
func TestContextClone_EventCopied(t *testing.T) {
	payload := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "original-id",
	}

	originalCtx := NewContext(payload, nil)
	clonedCtx := originalCtx.Clone()

	// 修改原始事件
	originalCtx.GetEvent().ID = "modified-id"

	// 验证克隆的事件未受影响
	if clonedCtx.GetEvent().ID != "original-id" {
		t.Errorf("Cloned event should not be affected by original event modification, got: %s", clonedCtx.GetEvent().ID)
	}

	t.Log("✓ Event properly cloned and independent")
}
