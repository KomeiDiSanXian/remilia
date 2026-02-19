package context

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestContextClone_Simple 简单的克隆测试
func TestContextClone_Simple(t *testing.T) {
	payload := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-123",
	}

	originalCtx := NewContext(payload, nil)
	clonedCtx := originalCtx.Clone()

	if clonedCtx == nil {
		t.Fatal("Cloned context should not be nil")
	}

	if clonedCtx.GetEvent().ID != "test-123" {
		t.Errorf("Expected event ID 'test-123', got '%s'", clonedCtx.GetEvent().ID)
	}

	t.Log("✓ Simple clone test passed")
}

// TestContextClone_IndependentContext 测试独立的 context
func TestContextClone_IndependentContext(t *testing.T) {
	stdCtx, cancel := context.WithCancel(context.Background())

	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	originalCtx := NewContextWithContext(stdCtx, payload, nil)
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
		t.Error("Cloned context should NOT be canceled")
	default:
		// 期望行为
	}

	t.Log("✓ Independent context test passed")
}
