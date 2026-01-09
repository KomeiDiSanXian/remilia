package remilia

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
)

type testPluginGroupSemantics struct {
	*BasePlugin
	onLoad func(e *Engine)
}

func (p *testPluginGroupSemantics) Load(e *Engine) error {
	if p.onLoad != nil {
		p.onLoad(e)
	}
	return nil
}

func (p *testPluginGroupSemantics) Unload(e *Engine) error { return p.BasePlugin.Unload(e) }

// This test locks down the contract:
// - matcher.group is authoritative for group middleware/unload
// - matcher.Source is diagnostics only and MUST NOT drive group assignment.
func TestPlugin_GroupIsAuthoritative_SourceIsNot(t *testing.T) {
	engine := NewEngine()
	pm := NewPluginManager(engine)

	var called int32

	// global matcher
	engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt32(&called, 1)
	})

	// plugin with one matcher
	p := &testPluginGroupSemantics{BasePlugin: NewBasePlugin("p1")}
	p.onLoad = func(e *Engine) {
		m := e.OnC2C().Handle(func(ctx *Context) {
			atomic.AddInt32(&called, 1)
		})
		p.AddMatcher(m)
	}

	require.NoError(t, pm.Register(p))

	// ensure plugin matcher has group set, and source is label only
	ms := p.GetMatchers()
	require.Len(t, ms, 1)
	require.Equal(t, "p1", ms[0].group)
	require.Equal(t, "plugin:p1", ms[0].Source)

	// register group middleware for p1 (should apply only to plugin matcher)
	engine.UseForGroup("p1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			ctx.Set("mw_hit", true)
			return next(ctx)
		}
	})

	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// Plugin matcher belongs to group p1, so group middleware must run.
	v, ok := ctx.Get("mw_hit")
	require.True(t, ok)
	require.Equal(t, true, v)

	// Now break the Source label intentionally; group behavior must remain stable.
	ms[0].Source = "plugin:renamed"
	engine.RebuildMatcherChain(ms[0])

	ctx2 := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx2)

	v2, ok := ctx2.Get("mw_hit")
	require.True(t, ok)
	require.Equal(t, true, v2)
}

// Regression test: if a matcher has Source set but group empty, it should NOT implicitly join that group.
func TestPlugin_SourceDoesNotBackfillGroup(t *testing.T) {
	engine := NewEngine()

	var hit int32

	// create a matcher that "looks like" plugin-labeled, but group is empty
	m := engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt32(&hit, 1)
	})
	m.Source = "plugin:p1"
	m.group = "" // explicitly empty
	engine.RebuildMatcherChain(m)

	engine.UseForGroup("p1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			ctx.Set("mw_hit", true)
			return next(ctx)
		}
	})

	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	_, ok := ctx.Get("mw_hit")
	require.False(t, ok)
	require.GreaterOrEqual(t, atomic.LoadInt32(&hit), int32(1))
}
