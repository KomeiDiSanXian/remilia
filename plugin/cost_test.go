package plugin

import (
	stdctx "context"
	"testing"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDescriptorCostAppliedToMatchers 插件声明的成本类别应由框架自动套到
// 它的 matcher 上：插件不需要在装配层登记，也不需要在 Setup 里逐个设置。
func TestDescriptorCostAppliedToMatchers(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers(), engine.WithExecPoolDisabled())
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	var matcher *engine.Matcher
	err := pm.RegisterBatch(stdctx.Background(), []*Descriptor{
		{
			Name: "cost-declared",
			Cost: corectx.CostIO,
			Setup: func(ctx *SetupContext) (any, error) {
				matcher = ctx.Reg.RegisterMatcher(testEvent)
				return nil, nil
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, matcher)
	assert.Equal(t, corectx.CostIO, matcher.Cost(), "descriptor cost must reach registered matchers")
}

// TestMatcherCostOverrideWins 混合成本插件可以覆盖单条路由（如 /ai help 便宜）。
func TestMatcherCostOverrideWins(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers(), engine.WithExecPoolDisabled())
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	var cheap, expensive *engine.Matcher
	err := pm.RegisterBatch(stdctx.Background(), []*Descriptor{
		{
			Name: "cost-mixed",
			Cost: corectx.CostIO,
			Setup: func(ctx *SetupContext) (any, error) {
				cheap = ctx.Reg.RegisterMatcher(testEvent).SetCost(corectx.CostLocal)
				expensive = ctx.Reg.RegisterMatcher(testEvent)
				return nil, nil
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cheap)
	require.NotNil(t, expensive)
	assert.Equal(t, corectx.CostLocal, cheap.Cost(), "explicit per-matcher cost must win")
	assert.Equal(t, corectx.CostIO, expensive.Cost(), "undeclared matcher inherits the plugin cost")
}
