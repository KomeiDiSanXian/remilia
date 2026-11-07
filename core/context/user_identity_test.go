package context

// user_identity_test.go — 用户身份的来源优先级
//
// 回归背景：GetUserID 优先取平台事件的发送者，SetUserID 写的是字符串
// 状态键——在平台事件路径下 SetUserID 静默无效，且此前没有文档说明。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestContext_SetUserIDDoesNotShadowPlatformSender 验证平台事件路径下
// SetUserID 不会改变 GetUserID 的结果（身份来自事件本身）。
func TestContext_SetUserIDDoesNotShadowPlatformSender(t *testing.T) {
	evt := newMockEventWithSender(platform.EventKindPrivateMessage, "real-user")
	ctx := NewContextFromEvent(evt, nil)

	assert.Equal(t, "real-user", ctx.GetUserID())
	ctx.SetUserID("spoofed")
	assert.Equal(t, "real-user", ctx.GetUserID(),
		"平台事件的发送者优先，SetUserID 不得覆盖")
}

// TestContext_SetUserIDAppliesWithoutPlatformEvent 验证无事件绑定时
// SetUserID 仍然生效。
func TestContext_SetUserIDAppliesWithoutPlatformEvent(t *testing.T) {
	ctx := newTestCtxEmpty()

	ctx.SetUserID("manual-user")

	assert.Equal(t, "manual-user", ctx.GetUserID())
}
