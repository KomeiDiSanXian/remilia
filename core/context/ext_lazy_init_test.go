package context

// ext_lazy_init_test.go — 扩展存储的惰性初始化边界
//
// 回归背景：Get / GetString / GetRetryAttempt / CandidateMeta 等纯读 API
// 都会先调 Ext()，即使该 Context 从未写过扩展也会分配 Extensions + map。
// 对"只回复、不存状态"的 handler 是每事件一次的纯浪费。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContext_ReadOnlyExtAPIsDoNotAllocate 验证只读扩展 API 不会触发
// 扩展存储的惰性初始化。
func TestContext_ReadOnlyExtAPIsDoNotAllocate(t *testing.T) {
	ctx := newTestCtx()

	_, _ = ctx.Get("missing")
	_ = ctx.GetString("missing")
	_ = ctx.All()
	_, _ = ctx.GetRetryAttempt()
	_, _ = ctx.GetMiddlewareTrace()
	_ = ctx.GetParsedCommand()
	_ = ctx.CandidateMeta()
	_ = ctx.GetPermissionManager()
	ctx.Delete("missing")
	ctx.SetCandidateMeta(nil) // 清除一个从未存在的 Meta 不应建表

	assert.False(t, ctx.extInitialized.Load(),
		"纯读 API 不应触发扩展存储的惰性初始化")
}

// TestContext_WriteThenReadAllocatesOnce 验证首次写入后读写都复用同一份存储。
func TestContext_WriteThenReadAllocatesOnce(t *testing.T) {
	ctx := newTestCtx()

	ctx.Set("k", "v")
	first := ctx.Ext()
	assert.True(t, ctx.extInitialized.Load())
	assert.Same(t, first, ctx.Ext(), "Ext() 必须返回同一份存储")

	assert.Equal(t, "v", ctx.GetString("k"))
}
