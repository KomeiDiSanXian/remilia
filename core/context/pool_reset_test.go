package context

// pool_reset_test.go — Context 生命周期测试

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestNewContext_FieldsAccessible 验证 Release 后字段仍可访问（无池化复用）。
func TestNewContext_FieldsAccessible(t *testing.T) {
	event := newMockEvent(platform.EventKindPrivateMessage)
	ctx := NewContextFromEvent(event, nil)

	assert.NotNil(t, ctx.platformEvent, "platformEvent 应可直接访问")
	assert.NotNil(t, ctx.GetPlatformEvent(), "GetPlatformEvent() 可获取事件")
}

// TestNewContext_ExtNotNil 验证新鲜分配的 Ext() 不为 nil。
func TestNewContext_ExtNotNil(t *testing.T) {
	ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	ext := ctx.Ext()
	assert.NotNil(t, ext)
}

// TestMultiCycle 多轮 acquire，验证每轮都正常工作。
func TestMultiCycle(t *testing.T) {
	for range 10 {
		ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
		ext := ctx.Ext()
		assert.NotNil(t, ext)
		type cycleKey struct{}
		ctx.Ext().SetTyped(cycleKey{})
	}
}
