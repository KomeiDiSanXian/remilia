package context

// context_escape_test.go — Clone 逃逸优化回归测试
//
// copyExtensions 空存储短路后：
//   - 克隆未使用扩展的 Context 不再初始化两侧扩展存储（行为断言）
//   - 每事件 Clone 分配大幅下降（AllocsPerRun 断言）
//   - 基准持续监控分配预算，防止回归

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContextClone_NoExtensions_NoInit 验证克隆未使用扩展的 Context 时，
// 源与目标的扩展存储都保持未初始化（此前会触发两侧惰性初始化分配）。
func TestContextClone_NoExtensions_NoInit(t *testing.T) {
	orig := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), &platform.NoopSender{})

	cloned := orig.Clone()

	assert.False(t, orig.extInitialized.Load(), "source extension storage must stay uninitialized")
	assert.False(t, cloned.extInitialized.Load(), "cloned extension storage must stay uninitialized")

	// 按需访问语义保持：调用 Ext() 后仍能获得可用存储
	assert.NotNil(t, cloned.Ext())
}

// TestContextClone_Allocs_NoExtensions 验证裸 Clone 分配预算（回归护栏）。
//
// 优化前：~7 allocs/op（src Extensions+map、dst Extensions+map、Snapshot 等）
// 优化后：3 allocs/op（&Context{}、WithCancel cancelCtx、&ctxHolder{}）
func TestContextClone_Allocs_NoExtensions(t *testing.T) {
	orig := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), &platform.NoopSender{})

	allocs := testing.AllocsPerRun(2000, func() {
		c := orig.Clone()
		c.Cancel()
	})
	assert.LessOrEqual(t, allocs, 4.0, "bare clone should stay within allocation budget")
}

// TestContextClone_ExtensionsCopied 验证有扩展时的克隆语义保持不变
// （类型键 + 字符串键两套存储都要深拷贝，且修改克隆不影响源）。
func TestContextClone_ExtensionsCopied(t *testing.T) {
	orig := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), &platform.NoopSender{})
	orig.Set("chat_name", "测试群")
	orig.Set("group_role", "member")
	type typedKey struct{ n int }
	ExtSet(orig.Ext(), typedKey{n: 42})

	cloned := orig.Clone()

	// 字符串键隔离
	v, ok := cloned.Get("chat_name")
	require.True(t, ok, "string key must be copied to clone")
	assert.Equal(t, "测试群", v)
	cloned.Set("chat_name", "另一个群")
	origV, _ := orig.Get("chat_name")
	assert.Equal(t, "测试群", origV, "mutating clone must not affect source")

	// 类型键隔离
	tv, ok := ExtGet[typedKey](cloned.Ext())
	require.True(t, ok, "typed key must be copied to clone")
	assert.Equal(t, 42, tv.n)
	ExtSet(cloned.Ext(), typedKey{n: 1})
	tv2, _ := ExtGet[typedKey](orig.Ext())
	assert.Equal(t, 42, tv2.n, "mutating clone typed ext must not affect source")
}

// ── 分配预算基准 ──────────────────────────────────────────────────────────

func benchCloneBudget(b *testing.B, setup func(c *Context)) {
	b.Helper()
	orig := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), &platform.NoopSender{})
	setup(orig)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = orig.Clone()
	}
}

// BenchmarkContextCloneBare 无任何扩展的 Clone（纯回复 handler 的常见形态）。
// 优化前 ~7 allocs/op，优化后预期 3 allocs/op。
func BenchmarkContextCloneBare(b *testing.B) {
	benchCloneBudget(b, func(c *Context) {})
}

// BenchmarkContextCloneWithStdCtx 带 tracing/deadline 中间件注入的 Clone。
func BenchmarkContextCloneWithStdCtx(b *testing.B) {
	benchCloneBudget(b, func(c *Context) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.SetStdContext(ctx)
	})
}

// BenchmarkContextCloneWithExtensions 带字符串键扩展的 Clone。
func BenchmarkContextCloneWithExtensions(b *testing.B) {
	benchCloneBudget(b, func(c *Context) {
		c.Set("chat_name", "测试群")
		c.Set("group_role", "member")
		c.Set("mention_count", 2)
	})
}

// BenchmarkContextCloneFull 完整形态（stdctx + 扩展）的 Clone。
func BenchmarkContextCloneFull(b *testing.B) {
	benchCloneBudget(b, func(c *Context) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.SetStdContext(ctx)
		c.Set("chat_name", "测试群")
		c.Set("group_role", "member")
		c.Set("mention_count", 2)
	})
}
