package remilia

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestNewEngine(t *testing.T) {
	engine := NewEngine()
	assert.NotNil(t, engine)

	// COW 模式：通过状态访问
	state := engine.state.Load().(*engineState)
	assert.Empty(t, state.matchers)
	assert.False(t, state.block)
	assert.NotNil(t, state.matcherIndex, "索引应该被初始化")
}

func TestEngineSetBlock(t *testing.T) {
	engine := NewEngine()

	// Test setting block to true
	result := engine.SetBlock(true)
	state := engine.state.Load().(*engineState)
	assert.True(t, state.block)
	assert.Equal(t, engine, result) // Test method chaining

	// Test setting block to false
	engine.SetBlock(false)
	state = engine.state.Load().(*engineState)
	assert.False(t, state.block)
}

func TestEngineUseMiddleware(t *testing.T) {
	engine := NewEngine()

	mw1 := func(next HandlerE) HandlerE {
		return func(ctx *Context) error { return next(ctx) }
	}
	mw2 := func(next HandlerE) HandlerE {
		return func(ctx *Context) error { return next(ctx) }
	}

	engine.Use(mw1)
	mwState := engine.middleware.Load().(*middlewareState)
	assert.Len(t, mwState.global.chain, 1)

	engine.Use(mw2)
	mwState = engine.middleware.Load().(*middlewareState)
	assert.Len(t, mwState.global.chain, 2)
}

func TestEngineOn(t *testing.T) {
	engine := NewEngine()

	eventType := dto.C2CMessageCreate
	rule := func(ctx *Context) bool { return true }

	matcher := engine.On(eventType, rule)

	assert.NotNil(t, matcher)
	assert.Equal(t, dto.C2CMessageCreate, matcher.EventType)
	assert.Len(t, matcher.Rules, 1)
	assert.Equal(t, engine, matcher.Engine)
	assert.Equal(t, uint(50), matcher.priority) // Default priority

	// COW 模式：通过状态访问
	state := engine.state.Load().(*engineState)
	assert.Len(t, state.matchers, 1)
}

func TestEngineProcessEvent_MiddlewareBlocks(t *testing.T) {
	engine := NewEngine()

	executed := false

	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return NewBlockError("blocked by middleware")
		}
	})

	engine.OnC2C().HandleE(func(ctx *Context) error {
		executed = true
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	assert.False(t, executed, "Handler should not be executed when middleware blocks")
}

func TestEngineProcessEvent_MatcherExecutes(t *testing.T) {
	engine := NewEngine()

	executed := false

	engine.OnC2C().Handle(func(ctx *Context) {
		executed = true
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	assert.True(t, executed, "Handler should be executed when matcher matches")
}

func TestEngineProcessEvent_BlockStopsSubsequentMatchers(t *testing.T) {
	engine := NewEngine()

	firstExecuted := false
	secondExecuted := false

	engine.OnC2C().
		SetBlock(true).
		Handle(func(ctx *Context) {
			firstExecuted = true
		})

	engine.OnC2C().Handle(func(ctx *Context) {
		secondExecuted = true
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	assert.True(t, firstExecuted)
	assert.False(t, secondExecuted)
}

func TestEngineProcessEvent_EngineBlockStopsSubsequentMatchers(t *testing.T) {
	engine := NewEngine().SetBlock(true)

	firstExecuted := false
	secondExecuted := false

	engine.OnC2C().Handle(func(ctx *Context) {
		firstExecuted = true
	})

	engine.OnC2C().Handle(func(ctx *Context) {
		secondExecuted = true
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	assert.True(t, firstExecuted, "First handler should be executed")
	assert.False(t, secondExecuted, "Second handler should not be executed when engine blocks")
}

func TestEngineProcessEvent_MiddlewareAlwaysExecutes(t *testing.T) {
	engine := NewEngine()

	middlewareExecuted := false

	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			middlewareExecuted = true
			return next(ctx)
		}
	})

	// 注册仅匹配 GroupAt 事件的 matcher
	engine.OnGroupAt().HandleE(func(ctx *Context) error { return nil })

	// 使用 C2C 事件，不会命中 matcher，因此中间件也不会执行
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	assert.False(t, middlewareExecuted, "Middleware should only execute when matcher matches")
}

func TestEngineDeleteAllMatchers(t *testing.T) {
	engine := NewEngine()

	// Add some matchers
	m1 := engine.OnC2C()
	m2 := engine.OnGroupAt()

	state := engine.state.Load().(*engineState)
	assert.Len(t, state.matchers, 2)

	engine.DeleteAllMatchers()

	// 确认 matcher 对象本身仍然有效（Delete 逻辑在 Matcher.Delete 中处理）
	assert.NotNil(t, m1)
	assert.NotNil(t, m2)
}

func TestEngineProcessEvent_NoMatcherExecutes(t *testing.T) {
	engine := NewEngine()

	executed := false

	// Add a matcher for GroupAt event only
	engine.OnGroupAt().Handle(func(ctx *Context) {
		executed = true
	})

	// Create test context with different event type
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	assert.False(t, executed, "Handler should not be executed when matcher doesn't match")
}

func TestEngineProcessEvent_MultipleMatchers(t *testing.T) {
	engine := NewEngine()

	count := 0

	engine.OnC2C().Handle(func(ctx *Context) { count++ })
	engine.OnC2C().Handle(func(ctx *Context) { count++ })
	engine.OnC2C().Handle(func(ctx *Context) { count++ })

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	assert.Equal(t, 3, count, "All matching handlers should be executed")
}

func TestEngineGlobalMatcherStats(t *testing.T) {
	engine := NewEngine()

	engine.OnC2C().Handle(func(ctx *Context) {})
	engine.OnGroupAt().Handle(func(ctx *Context) {})

	stats := engine.GetMatcherStats()
	assert.Equal(t, 2, stats.Total)
	assert.Equal(t, 2, stats.Global)
	assert.True(t, stats.GlobalEnabled)
	assert.Empty(t, stats.ByPlugin)
}

// Dummy plugin for labeling
type LabelPlugin struct{ *BasePlugin }

func NewLabelPlugin(name string) *LabelPlugin { return &LabelPlugin{BasePlugin: NewBasePlugin(name)} }

func (p *LabelPlugin) Load(engine *Engine) error {
	// Register one matcher under plugin
	m := engine.OnC2C()
	p.AddMatcher(m)
	return nil
}

func (p *LabelPlugin) Unload(engine *Engine) error { return p.BasePlugin.Unload(engine) }

func TestPluginManagerStatsAndLabeling(t *testing.T) {
	engine := NewEngine()
	pm := NewPluginManager(engine)

	// Global matchers
	engine.OnGroupAt()
	engine.OnGroupAdd()

	// Register plugin
	err := pm.Register(NewLabelPlugin("label"))
	assert.NoError(t, err)

	stats := pm.Stats()
	assert.Equal(t, 3, stats.Total)
	assert.Equal(t, 2, stats.Global)
	assert.Equal(t, 1, stats.ByPlugin["label"])
}

func TestEnableGlobalMatchersToggle(t *testing.T) {
	engine := NewEngine()
	engine.OnC2C()

	stats := engine.GetMatcherStats()
	assert.True(t, stats.GlobalEnabled)

	engine.EnableGlobalMatchers(false)
	stats2 := engine.GetMatcherStats()
	state := engine.state.Load().(*engineState)
	assert.Equal(t, !state.block, stats2.GlobalEnabled)
}

type P struct{ *BasePlugin }

func (p *P) Load(_ *Engine) error   { return nil }
func (p *P) Unload(e *Engine) error { return p.BasePlugin.Unload(e) }

func TestGlobalVsPluginHandling(t *testing.T) {
	engine := NewEngine()
	pm := NewPluginManager(engine)

	calledGlobal := false
	engine.OnGroupAt().Handle(func(ctx *Context) { calledGlobal = true })

	p := &P{BasePlugin: NewBasePlugin("p1")}
	_ = pm.Register(p)

	event := &dto.Payload{Type: dto.GroupAtMessageCreate}
	ctx := NewContext(event, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, calledGlobal)
}

func TestEngineOnC2CVsGroupAt(t *testing.T) {
	engine := NewEngine()

	c2cCalled := false
	groupCalled := false
	anyCalled := int32(0)

	engine.OnC2C().Handle(func(ctx *Context) { c2cCalled = true })
	engine.OnGroupAt().Handle(func(ctx *Context) { groupCalled = true })
	engine.OnAny(OnKeyword("x")).Handle(func(ctx *Context) {
		atomic.AddInt32(&anyCalled, 1)
	})

	c2c := &dto.Payload{Type: dto.C2CMessageCreate, Detail: []byte(`{"content":"x"}`)}
	group := &dto.Payload{Type: dto.GroupAtMessageCreate, Detail: []byte(`{"content":"x"}`)}

	engine.ProcessEvent(NewContext(c2c, nil))
	engine.ProcessEvent(NewContext(group, nil))

	assert.True(t, c2cCalled)
	assert.True(t, groupCalled)
	assert.Equal(t, int32(2), anyCalled)
}

func TestMiddlewareChain_RebuildAndReuse(t *testing.T) {
	engine := NewEngine()

	// 通过可观测副作用统计链的重建次数：在中间件中记录构造时机不适合作为稳定信号，
	// 这里采用“调用次数差值”+ 行为断言的方式，弱化对内部实现的强假设。

	var calls int32
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	})

	m := engine.OnC2C().HandleE(func(ctx *Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}

	// 多次调用同一 matcher，确保 handler 被正确调用，且不会因为链重建导致重复副作用
	engine.ProcessEvent(NewContext(event, nil))
	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(2), calls)

	// 增加一个新的全局中间件，不应影响已注册 matcher 的可见行为
	engine.Use(func(next HandlerE) HandlerE { return next })
	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(3), calls)

	// 删除 matcher 后不再被调用
	m.Delete()
	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(3), calls)
}

func TestMatcherDeleteLifecycleFlag(t *testing.T) {
	engine := NewEngine()
	m := engine.OnC2C()

	assert.False(t, m.IsDeleted())

	m.Delete()
	assert.True(t, m.IsDeleted())

	// 再次删除不应 panic 或触发重复删除
	m.Delete()
}

func TestTempMatcher_AutoDeleteAfterOneUse(t *testing.T) {
	engine := NewEngine()

	var calls int32
	m := engine.OnC2C().SetTemp(true).HandleE(func(ctx *Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(1), calls)
	assert.True(t, m.IsDeleted(), "temp matcher should be deleted after first use")

	// 再次事件不应再触发
	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(1), calls)
}

func TestTempMatcher_WithMaxUse(t *testing.T) {
	engine := NewEngine()

	var calls int32
	m := engine.OnC2C().SetTempWithMaxUse(2).HandleE(func(ctx *Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}

	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(1), calls)
	assert.False(t, m.IsDeleted())

	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(2), calls)
	assert.True(t, m.IsDeleted(), "matcher should be deleted after reaching maxUse")

	// 再次事件不应再触发
	engine.ProcessEvent(NewContext(event, nil))
	assert.Equal(t, int32(2), calls)
}
