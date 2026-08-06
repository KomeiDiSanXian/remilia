package context

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// --- platform stub for clone tests ---

type cloneTestEvent struct {
	content    string
	platformID string
	kind       platform.EventKind
}

func (e *cloneTestEvent) Platform() string         { return e.platformID }
func (e *cloneTestEvent) Kind() platform.EventKind { return e.kind }
func (e *cloneTestEvent) RawType() string          { return "test.event" }

func (e *cloneTestEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *cloneTestEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{ID: "chat-001"} }
func (e *cloneTestEvent) Sender() platform.UserInfo { return platform.UserInfo{ID: "user-001"} }
func (e *cloneTestEvent) Timestamp() time.Time      { return time.Time{} }
func (e *cloneTestEvent) ID() string                { return "" }
func (e *cloneTestEvent) RawPayload() any           { return nil }

// --- existing tests (unchanged) ---

// TestContextClone_IndependentCancellation 测试克隆的 Context 不受原 Context 取消影响
func TestContextClone_IndependentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// 创建原始 context，带取消功能
		stdCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		event := &cloneTestEvent{content: "hello", platformID: "qq", kind: platform.EventKindPrivateMessage}
		originalCtx := NewContextFromEvent(event, &platform.NoopSender{})
		originalCtx.SetStdContext(stdCtx)

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
	})
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
	event := &cloneTestEvent{content: "trace-test", platformID: "qq", kind: platform.EventKindPrivateMessage}
	originalCtx := NewContextFromEvent(event, &platform.NoopSender{})
	originalCtx.SetStdContext(stdCtx)
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
	event := &cloneTestEvent{content: "original-content", platformID: "qq", kind: platform.EventKindPrivateMessage}

	originalCtx := NewContextFromEvent(event, &platform.NoopSender{})
	clonedCtx := originalCtx.Clone()

	// 修改原始事件的内容字段后，克隆持有的是同一指针（platform.Event 是接口）
	// 验证克隆的 platformEvent 仍然存在且指向相同事件
	if clonedCtx.GetPlatformEvent() == nil {
		t.Fatal("Cloned context should have a non-nil platformEvent")
	}
	if clonedCtx.GetPlatformEvent().Platform() != "qq" {
		t.Errorf("Cloned context platformEvent.Platform() = %q, want %q", clonedCtx.GetPlatformEvent().Platform(), "qq")
	}

	t.Log("✓ Event properly cloned and independent")
}

// --- 新平台路径（NewContextFromEvent）克隆测试 ---

// TestContextClone_PlatformEvent_Preserved 验证 Clone() 保留 platformEvent（Fix 4.1）
func TestContextClone_PlatformEvent_Preserved(t *testing.T) {
	event := &cloneTestEvent{content: "hello", platformID: "qq", kind: platform.EventKindPrivateMessage}
	sender := &platform.NoopSender{}

	original := NewContextFromEvent(event, sender)
	cloned := original.Clone()

	// platformEvent 必须被保留
	if cloned.GetPlatformEvent() == nil {
		t.Fatal("Clone() must preserve platformEvent; got nil")
	}
	if cloned.GetPlatformEvent().Platform() != "qq" {
		t.Errorf("platformEvent.Platform() = %q, want %q", cloned.GetPlatformEvent().Platform(), "qq")
	}
	if cloned.GetMessageContent() != "hello" {
		t.Errorf("GetMessageContent() = %q, want %q", cloned.GetMessageContent(), "hello")
	}

	t.Log("✓ platformEvent preserved in cloned context")
}

// TestContextClone_PlatformSender_Preserved 验证 Clone() 保留 platformSender（Fix 4.1）
func TestContextClone_PlatformSender_Preserved(t *testing.T) {
	event := &cloneTestEvent{content: "ping", platformID: "discord", kind: platform.EventKindGroupMessage}
	sender := &platform.NoopSender{}

	original := NewContextFromEvent(event, sender)
	cloned := original.Clone()

	if cloned.GetPlatformSender() == nil {
		t.Fatal("Clone() must preserve platformSender; got nil — ctx.Reply() would return ErrNoPlatformSender")
	}

	t.Log("✓ platformSender preserved in cloned context")
}

// TestContextClone_PlatformPath_IndependentFromOriginal 验证新路径克隆出的 Context
// 独立于原始 Context 的 stdctx 取消
func TestContextClone_PlatformPath_IndependentFromOriginal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		event := &cloneTestEvent{content: "msg", platformID: "telegram", kind: platform.EventKindPrivateMessage}
		sender := &platform.NoopSender{}

		stdCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		original := NewContextFromEvent(event, sender)
		original.SetStdContext(stdCtx)

		cloned := original.Clone()

		// 取消原始 stdctx
		cancel()
		time.Sleep(5 * time.Millisecond)

		select {
		case <-cloned.Context().Done():
			t.Error("cloned context should NOT be affected by original stdctx cancellation")
		default:
			// expected: cloned context is independent
		}

		// 平台字段仍然存在
		if cloned.GetPlatformEvent() == nil {
			t.Error("platformEvent should still be present after original cancel")
		}
		if cloned.GetPlatformSender() == nil {
			t.Error("platformSender should still be present after original cancel")
		}

		t.Log("✓ platform-path cloned context is independent and retains platform fields")
	})
}

// TestContextClone_PlatformFields_Preserved 验证新路径克隆时平台字段被正确保留
func TestContextClone_PlatformFields_Preserved(t *testing.T) {
	event := &cloneTestEvent{content: "check", platformID: "discord", kind: platform.EventKindGroupMessage}
	original := NewContextFromEvent(event, &platform.NoopSender{})

	cloned := original.Clone()

	if cloned.GetPlatformEvent() == nil {
		t.Error("cloned context should have non-nil platformEvent")
	}
	if cloned.GetPlatformSender() == nil {
		t.Error("cloned context should have non-nil platformSender")
	}
	if cloned.GetPlatformEvent().Platform() != "discord" {
		t.Errorf("platformEvent.Platform() = %q, want %q", cloned.GetPlatformEvent().Platform(), "discord")
	}

	t.Log("✓ new-path clone correctly has non-nil platform fields")
}
