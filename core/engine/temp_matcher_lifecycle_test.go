package engine

// temp_matcher_lifecycle_test.go — 临时 matcher 的删除与生命周期
//
// 回归背景：Matcher.Delete() 只把 matcher 从 COW state 里摘除，对
// TempManager 中的临时 matcher 完全无效——只能等超时清理，期间仍会路由
// （sauce 的"手动取消立即移除"因此长期不成立）。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMatcher_DeleteTempMatcherTakesEffect 验证 OnTemp 创建的 matcher
// 调用 Delete() 后立即从 TempManager 摘除。
func TestMatcher_DeleteTempMatcherTakesEffect(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m := eng.OnTemp(et)
	m.Handle(func(*context.Context) error { return nil })
	require.Equal(t, 1, eng.GetTempMatcherCount())

	m.Delete()

	assert.Equal(t, 0, eng.GetTempMatcherCount(), "Delete() 必须把临时 matcher 从 TempManager 摘除")
	assert.Empty(t, eng.internals.tempManager.Get(et), "RCU 快照不得继续路由已删除的临时 matcher")
}
