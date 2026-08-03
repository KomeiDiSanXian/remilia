package engine

import (
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterBatch_VisibilityAfterFlush(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	ran := 0

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()
	m := e.On(et)
	m.Handle(func(ctx *context.Context) error {
		ran++
		return nil
	})

	// Flush 前不可路由
	e.ProcessEvent(newTestCtx("hello"))
	assert.Zero(t, ran, "batch matcher must not be routable before Flush")

	batch.Flush()
	e.ProcessEvent(newTestCtx("hello"))
	assert.Equal(t, 1, ran, "batch matcher must be routable after Flush")
}

func TestRegisterBatch_CommandAndGroup(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	ran := false

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()
	m := e.OnCommand(et, "/ping")
	e.SetMatcherGroup(m, "plugin:test", "plugin:test")
	m.Handle(func(ctx *context.Context) error {
		ran = true
		return nil
	})
	batch.Flush()

	// 命令路由生效
	e.ProcessEvent(newTestCtx("/ping args"))
	assert.True(t, ran)

	// groupIndex 正确建立：DisableGroup 后命令不再响应
	e.DisableGroup("plugin:test")
	ran = false
	e.ProcessEvent(newTestCtx("/ping args"))
	assert.False(t, ran, "batched matcher must respect group disable")
}

func TestRegisterBatch_RegularMatcherHandlerFilter(t *testing.T) {
	// 批内普通 matcher：Handle 在批期调用，Flush 时 hasHandler 过滤需正确
	et := string(platform.EventKindPrivateMessage)
	withHandler := false
	withoutHandler := false

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()
	m1 := e.On(et)
	m1.Handle(func(ctx *context.Context) error {
		withHandler = true
		return nil
	})
	e.On(et) // 无 handler 的 matcher 不应被路由
	batch.Flush()

	e.ProcessEvent(newTestCtx("hello"))
	assert.True(t, withHandler)
	assert.False(t, withoutHandler)
}

func TestRegisterBatch_TempMatcher(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)
	ran := false

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()
	m := e.OnTemp(et, context.OnKeyword("temp"))
	m.Handle(func(ctx *context.Context) error {
		ran = true
		return nil
	})
	batch.Flush()

	e.ProcessEvent(newTestCtx("temp keyword"))
	assert.True(t, ran, "batched temp matcher must route after Flush")
}

func TestRegisterBatch_IdempotentFlush(t *testing.T) {
	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()
	e.On(string(platform.EventKindPrivateMessage))
	batch.Flush()
	batch.Flush() // 幂等

	// 再次 Begin 是全新会话
	batch2 := e.BeginRegisterBatch()
	require.NotSame(t, batch, batch2)
	batch2.Flush()
}

func TestRegisterBatch_ConcurrentOverlapDegrades(t *testing.T) {
	// 已有活跃会话时 Begin 返回 nil：并发加载重叠退化为逐条注册
	e := newEngineForTest(t)
	first := e.BeginRegisterBatch()
	require.NotNil(t, first)

	second := e.BeginRegisterBatch()
	assert.Nil(t, second, "concurrent batch must not be created (engine-level singleton)")

	first.Flush()

	// 会话结束后可再次开启
	third := e.BeginRegisterBatch()
	require.NotNil(t, third)
	third.Flush()
}

func TestRegisterBatch_InvalidateDirtyDuringBatch(t *testing.T) {
	// 批期 Handle() 触发的 InvalidateSortedCache 降级为收集，
	// Flush 后排序缓存包含新 matcher
	et := string(platform.EventKindPrivateMessage)
	ran := false

	e := newEngineForTest(t)
	batch := e.BeginRegisterBatch()
	m := e.On(et)
	m.Handle(func(ctx *context.Context) error {
		ran = true
		return nil
	})
	batch.Flush()

	e.ProcessEvent(newTestCtx("hello"))
	assert.True(t, ran, "sortedCache must include batched matcher after Flush")
}

// ---- 回归基准：顺序注册写放大 --------------------------------------------------

func BenchmarkRegisterBatch_Sequential1000Commands(b *testing.B) {
	for i := 0; i < b.N; i++ {
		e := newEngineForTest(b, WithNoBackgroundWorkers())
		for j := range 1000 {
			m := e.OnCommand("", fmt.Sprintf("/cmd%d", j))
			m.Handle(func(c *context.Context) error { return nil })
		}
	}
}

func BenchmarkRegisterBatch_Batched1000Commands(b *testing.B) {
	for i := 0; i < b.N; i++ {
		e := newEngineForTest(b, WithNoBackgroundWorkers())
		batch := e.BeginRegisterBatch()
		for j := range 1000 {
			m := e.OnCommand("", fmt.Sprintf("/cmd%d", j))
			m.Handle(func(c *context.Context) error { return nil })
		}
		batch.Flush()
	}
}
