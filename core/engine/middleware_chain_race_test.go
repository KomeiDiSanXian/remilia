package engine

import (
	"sync"
	"sync/atomic"
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 中间件链缓存一致性回归测试。
//
// 回归背景：invokeHandler 慢路径先在 ensureMatcherChain 时读取中间件状态，
// 再把旧链写入 compiledHandlers，而版本号取自失效之后的新值。此后热路径
// 版本比对一直命中，matcher 会永久跳过在其中途注册的中间件（含鉴权、日志）。

// TestChainSnapshotValid_RejectsStaleMiddlewareSnapshot 验证「新版本号 + 旧链」
// 的快照会被判定为无效。这是竞态最危险的组合：版本比对骗过热路径。
func TestChainSnapshotValid_RejectsStaleMiddlewareSnapshot(t *testing.T) {
	eng := newEngineForTest(t)

	m := eng.On(string(platform.EventKindPrivateMessage))
	m.Handle(func(*ctx.Context) error {
		return nil
	})

	// 先建立一份与当前中间件状态一致的快照
	eng.Use(func(next ctx.Handler) ctx.Handler { return next })
	mwState := eng.middleware.Load()
	eng.ensureMatcherChainWithState(m, mwState)
	chain, globalGen, groupGen := m.getChainCache()
	require.NotNil(t, chain)
	cv := m.compiledVersion.Load()
	require.True(t, eng.chainSnapshotValid(m, cv, globalGen, groupGen))

	// 并发变更后，旧链 + 新版本号必须被拒绝
	eng.Use(func(next ctx.Handler) ctx.Handler { return next })
	assert.False(t,
		eng.chainSnapshotValid(m, m.compiledVersion.Load(), globalGen, groupGen),
		"带有失效代际号的链快照必须被拒绝，即使版本号取到了新值")

	// 重新建链后校验重新通过
	mwState = eng.middleware.Load()
	eng.ensureMatcherChainWithState(m, mwState)
	chain, globalGen, groupGen = m.getChainCache()
	require.NotNil(t, chain)
	assert.True(t, eng.chainSnapshotValid(m, m.compiledVersion.Load(), globalGen, groupGen))
}

// TestEngine_MiddlewareRegisteredDuringChainBuildApplies 复现构建期间的
// 并发中间件注册：中间件工厂函数在链构建过程中调用 Use（等价于另一协程恰好
// 在链快照与缓存落盘之间完成注册）。修复后，注册的中间件必须对当前事件
// 与后续热路径事件都生效。
func TestEngine_MiddlewareRegisteredDuringChainBuildApplies(t *testing.T) {
	eng := newEngineForTest(t)

	var once sync.Once
	var added atomic.Bool

	// 该中间件的工厂函数在被组合时注册一个新中间件；once 保证只注册一次，
	// 让重试路径可以收敛。
	eng.Use(func(next ctx.Handler) ctx.Handler {
		once.Do(func() {
			eng.Use(func(inner ctx.Handler) ctx.Handler {
				return func(c *ctx.Context) error {
					added.Store(true)
					return inner(c)
				}
			})
		})
		return next
	})

	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(*ctx.Context) error {
		return nil
	})

	// 首次事件触发慢路径构建；构建过程中注册的中间件必须立即生效
	eng.ProcessEvent(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	assert.True(t, added.Load(), "链构建期间注册的中间件应对当前事件生效")

	// 后续事件走热路径（compiledHandlers 命中），同样必须携带该中间件
	added.Store(false)
	eng.ProcessEvent(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	assert.True(t, added.Load(), "缓存链必须包含构建期间注册的中间件")
}

// TestEngine_MiddlewareAddedAfterFirstEventApplies 验证顺序场景：
// matcher 已编译出无中间件的缓存后，再注册中间件必须对后续事件生效。
func TestEngine_MiddlewareAddedAfterFirstEventApplies(t *testing.T) {
	eng := newEngineForTest(t)

	var handled atomic.Int32
	eng.On(string(platform.EventKindPrivateMessage)).Handle(func(*ctx.Context) error {
		handled.Add(1)
		return nil
	})

	eng.ProcessEvent(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	require.Equal(t, int32(1), handled.Load())

	var guarded atomic.Bool
	eng.Use(func(next ctx.Handler) ctx.Handler {
		return func(c *ctx.Context) error {
			guarded.Store(true)
			return next(c)
		}
	})

	eng.ProcessEvent(ctx.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil))
	assert.True(t, guarded.Load(), "新增中间件必须对已编译过缓存的 matcher 生效")
	assert.Equal(t, int32(2), handled.Load())
}
