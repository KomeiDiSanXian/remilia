package context

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestContextClone_Simple 简单的克隆测试
func TestContextClone_Simple(t *testing.T) {
	event := newMockEventWithID(platform.EventKindPrivateMessage, "test-123")
	originalCtx := NewContextFromEvent(event, nil)
	clonedCtx := originalCtx.Clone()

	if clonedCtx == nil {
		t.Fatal("Cloned context should not be nil")
	}

	if clonedCtx.GetPlatformEvent().ID() != "test-123" {
		t.Errorf("Expected event ID 'test-123', got '%s'", clonedCtx.GetPlatformEvent().ID())
	}

	t.Log("✓ Simple clone test passed")
}

// TestContextClone_IndependentContext 测试独立的 context
func TestContextClone_IndependentContext(t *testing.T) {
	stdCtx, cancel := context.WithCancel(context.Background())

	originalCtx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	originalCtx.SetStdContext(stdCtx)
	clonedCtx := originalCtx.Clone()

	// 取消原始 context
	cancel()

	// 验证原始已取消
	select {
	case <-originalCtx.Context().Done():
		// 期望行为
	default:
		t.Error("Original context should be canceled")
	}

	// 验证克隆未取消
	select {
	case <-clonedCtx.Context().Done():
		t.Error("Cloned context should not be canceled when original is canceled")
	default:
		// 期望行为
	}

	t.Log("✓ Independent context clone test passed")
}
