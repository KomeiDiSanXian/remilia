package plugin

import (
	stdctx "context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
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

// TestDescriptorCostAppliesToCommandMatchers 命令类 matcher 也必须继承
// Descriptor.Cost：业务插件几乎全部用 OnCommand/OnCommandDefWith 注册，
// 它们此前绕过 afterRegister，导致声明的成本类别对主流写法完全失效。
func TestDescriptorCostAppliesToCommandMatchers(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers(), engine.WithExecPoolDisabled())
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	var cmd, custom *engine.Matcher
	require.NoError(t, pm.RegisterBatch(stdctx.Background(), []*Descriptor{{
		Name: "cost-cmd",
		Cost: corectx.CostIO,
		Setup: func(ctx *SetupContext) (any, error) {
			cmd = ctx.Reg.RegisterCommand("", "/cost-cmd")
			// 给命令声明别名，验证别名路由同样继承成本类别
			cmd.SetDefinition(&command.Definition{Name: "cost-cmd", Aliases: []string{"cc"}})
			cmd.Handle(func(*corectx.Context) error { return nil })
			custom = ctx.Reg.RegisterMatcher("")
			return nil, nil
		},
	}}))

	require.NotNil(t, cmd)
	assert.Equal(t, corectx.CostIO, cmd.Cost(), "命令 matcher 必须继承 Descriptor.Cost")
	require.NotNil(t, custom)
	assert.Equal(t, corectx.CostIO, custom.Cost(), "其它注册方法也必须继承 Descriptor.Cost")

	// 别名 matcher 同样应带上成本类别（实例会追踪别名 matcher）
	inst, ok := pm.Get("cost-cmd")
	require.True(t, ok)
	found := false
	for _, m := range inst.GetMatchers() {
		def := m.GetDefinition()
		if def == nil || def.Name != "cc" {
			continue
		}
		found = true
		assert.Equal(t, corectx.CostIO, m.Cost(), "别名 matcher 必须继承 Descriptor.Cost")
	}
	assert.True(t, found, "别名路由应已注册")
}
