package engine

// state_snapshot_test.go — 引擎状态快照（Snapshot/Restore）与命令缓存的一致性
//
// 回归背景：
//   - Restore 只替换 state/middleware，已编译的中间件链缓存在 Matcher 上，
//     回滚后仍会按回滚前的链执行；
//   - withAddedMatcher 就地写旧 state 的 commandInfoCache，历史快照因此
//     能看到之后注册的命令，Restore 后再增量注册会把幽灵命令展开回来。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngine_RestoreInvalidatesCompiledChain 验证 Restore 会失效已编译的
// 中间件链（此前回滚后仍执行回滚前的中间件）。
func TestEngine_RestoreInvalidatesCompiledChain(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	var order []string
	m := eng.On(et)
	m.Handle(func(*context.Context) error {
		order = append(order, "handler")
		return nil
	})

	snap := eng.Snapshot()

	eng.Use(func(next context.Handler) context.Handler {
		return func(c *context.Context) error {
			order = append(order, "mw")
			return next(c)
		}
	})
	evt := func() *context.Context {
		return context.NewContextFromEvent(
			newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hi"), nil)
	}
	eng.ProcessEvent(evt())
	require.Equal(t, []string{"mw", "handler"}, order)

	eng.Restore(snap)
	order = order[:0]
	eng.ProcessEvent(evt())
	assert.Equal(t, []string{"handler"}, order,
		"Restore 后不得继续执行回滚掉的中间件链")
}

// TestEngine_SnapshotCommandCacheNotPolluted 验证快照的 commandInfoCache
// 不被后续注册/元数据变更污染，且 Restore 后不会复活幽灵命令。
func TestEngine_SnapshotCommandCacheNotPolluted(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m1 := eng.OnCommand(et, "/one")
	m1.Handle(func(*context.Context) error { return nil })

	snap := eng.Snapshot()

	m2 := eng.OnCommand(et, "/two")
	m2.Handle(func(*context.Context) error { return nil })
	m2.SetDescription("later")

	eng.Restore(snap)
	assert.Nil(t, eng.FindCommand("/two"), "快照不得包含后续注册的命令")

	m3 := eng.OnCommand(et, "/three")
	m3.Handle(func(*context.Context) error { return nil })

	assert.Nil(t, eng.FindCommand("/two"), "增量注册不得复活被回滚的命令")
	require.NotNil(t, eng.FindCommand("/one"))
	require.NotNil(t, eng.FindCommand("/three"))
}
