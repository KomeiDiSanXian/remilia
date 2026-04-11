package context

// pool_reset_test.go — 对象池字段重置的白盒测试
//
// 测试目标（T-2）：验证 ReleaseContext 正确重置所有字段，确保池化复用安全。
// 重点覆盖 C-3 修复：extInitialized 未重置导致复用时 Ext() 返回 nil 的问题。

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReleaseContext_ResetsExtInitialized 直接验证 C-3 修复：
// ReleaseContext 必须将 extInitialized 重置为 false，
// 防止池化复用时 Ext() 快路径返回 nil *Extensions。
func TestReleaseContext_ResetsExtInitialized(t *testing.T) {
	ctx := newTestCtx()

	// 触发扩展初始化（extInitialized → true, extensions != nil）
	ext := ctx.Ext()
	require.NotNil(t, ext, "setup: Ext() 应返回非 nil")
	require.True(t, ctx.extInitialized.Load(), "setup: Ext() 调用后 extInitialized 应为 true")
	require.NotNil(t, ctx.extensions, "setup: extensions 应已初始化")

	// 释放回对象池
	ReleaseContext(ctx)

	// C-3 修复验证：extInitialized 必须被重置为 false
	assert.False(t, ctx.extInitialized.Load(),
		"C-3 fix: ReleaseContext 必须将 extInitialized 重置为 false，"+
			"否则池化复用时 Ext() 会从快路径返回 nil")
	assert.Nil(t, ctx.extensions,
		"ReleaseContext 后 extensions 字段应为 nil")
}

// TestReleaseContext_CancelCalled 验证 M-5 修复：
// ReleaseContext 必须调用并清除 Clone() 存储的 cancel 函数，
// 以释放 WithDeadline 创建的 runtime timer 资源。
func TestReleaseContext_CancelCalled(t *testing.T) {
	// 使用带 deadline 的 stdctx，Clone() 会创建 WithDeadline 并存储 cancel
	parentCtx, parentCancel := stdctx.WithDeadline(stdctx.Background(), time.Now().Add(time.Hour))
	defer parentCancel()

	ctx := newTestCtx()
	ctx.SetStdContext(parentCtx)

	// Clone() 对带 deadline 的 context 会调用 stdctx.WithDeadline，并将 cancel 存入 cloned.cancel
	cloned := ctx.Clone()
	require.NotNil(t, cloned, "Clone() 应返回非 nil")
	require.NotNil(t, cloned.cancel, "带 deadline 的 Clone() 应存储 cancel 函数（M-5 fix）")

	// 释放克隆的 context
	ReleaseContext(cloned)

	// M-5 修复验证：cancel 应已被调用并置 nil
	assert.Nil(t, cloned.cancel,
		"M-5 fix: ReleaseContext 后 cancel 应为 nil（已调用并清除）")
}

// TestReleaseContext_ContentCacheReset 验证内容缓存（contentOnce + content）被正确清除。
func TestReleaseContext_ContentCacheReset(t *testing.T) {
	event := newMockEventWithContent(platform.EventKindPrivateMessage, "hello world")
	ctx := AcquireContextFromEvent(event, nil)
	defer func() {
		// ctx 已在此测试中被 Release，不需要再 Release
	}()

	// 触发内容缓存
	content := ctx.GetMessageContent()
	assert.Equal(t, "hello world", content, "setup: 消息内容应正确缓存")
	assert.Equal(t, "hello world", ctx.content, "setup: content 字段应已填充")

	// 释放
	ReleaseContext(ctx)

	// 内容缓存应被清除
	assert.Empty(t, ctx.content, "ReleaseContext 后 content 字段应被清空")
}

// TestReleaseContext_PlatformFieldsReset 验证平台相关字段被清除。
func TestReleaseContext_PlatformFieldsReset(t *testing.T) {
	event := newMockEvent(platform.EventKindPrivateMessage)
	ctx := AcquireContextFromEvent(event, nil)

	// 确认字段已填充
	assert.NotNil(t, ctx.platformEvent, "setup: platformEvent 应已设置")

	// 释放
	ReleaseContext(ctx)

	// 平台字段应被清除
	assert.Nil(t, ctx.platformEvent, "ReleaseContext 后 platformEvent 应为 nil")
	assert.Nil(t, ctx.platformSender, "ReleaseContext 后 platformSender 应为 nil")
	assert.Empty(t, ctx.botID, "ReleaseContext 后 botID 应为空字符串")
}

// TestPoolReuse_ExtNotNil 验证池化复用后 Ext() 不会返回 nil。
//
// 这是 C-3 bug 的行为回归测试：如果 extInitialized 未重置，复用时
// Ext() 的快路径会返回已被清空的 nil extensions 指针。
func TestPoolReuse_ExtNotNil(t *testing.T) {
	// 第一次使用：初始化 extensions
	ctx1 := newTestCtx()
	ext1 := ctx1.Ext()
	require.NotNil(t, ext1, "第一次 Ext() 应不为 nil")
	require.True(t, ctx1.extInitialized.Load())

	// 归还到池
	ReleaseContext(ctx1)

	// 第二次从池中获取（有可能是同一个对象被复用）
	ctx2 := newTestCtx()
	defer ReleaseContext(ctx2)

	// 关键断言：无论是否复用同一对象，Ext() 绝不能返回 nil
	ext2 := ctx2.Ext()
	require.NotNil(t, ext2,
		"C-3 fix: 池化复用后 Ext() 不得返回 nil"+
			"（需要 ReleaseContext 重置 extInitialized）")

	// 验证 ext 是干净的（无上次数据残留）
	snapshot := ext2.Snapshot()
	assert.Empty(t, snapshot, "池化复用的 context Ext() 应为空（无数据残留）")
}

// TestPoolReuse_MultiCycle 多轮 acquire-use-release 循环，验证池化状态始终干净。
// 此测试也可作为 -race 测试辅助验证。
func TestPoolReuse_MultiCycle(t *testing.T) {
	for i := range 10 {
		ctx := newTestCtx()

		// 每个 Context 都初始化 Ext 并写入数据
		ext := ctx.Ext()
		require.NotNil(t, ext, "cycle %d: Ext() 不应为 nil", i)

		// 写入类型键数据
		type cycleKey struct{ n int }
		ExtSet(ctx.Ext(), cycleKey{n: i})

		// 归还
		ReleaseContext(ctx)
	}

	// 最后一次 acquire，验证无数据残留
	ctx := newTestCtx()
	defer ReleaseContext(ctx)

	ext := ctx.Ext()
	require.NotNil(t, ext, "多轮复用后 Ext() 不应为 nil")
	snapshot := ext.Snapshot()
	assert.Empty(t, snapshot, "多轮复用后 Ext() 不应有任何历史数据残留")
}
