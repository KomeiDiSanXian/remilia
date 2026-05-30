package plugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== L3: DryRun / noopEventBus 测试 =====

// TestDryRun_SmartInferenceSetsFlag 验证 Smart 推断阶段 ctx.DryRun == true
func TestDryRun_SmartInferenceSetsFlag(t *testing.T) {
	pm := NewManager(nil)

	var dryRunSeen bool
	var realRunSeen bool

	desc := &Descriptor{
		Name: "dryrun-test",
		Setup: func(ctx *SetupContext) (any, error) {
			if ctx.DryRun {
				dryRunSeen = true
			} else {
				realRunSeen = true
			}
			return nil, nil
		},
	}

	err := pm.RegisterMultipleSmart([]*Descriptor{desc})
	require.NoError(t, err)

	assert.True(t, dryRunSeen, "Smart 推断阶段应设置 ctx.DryRun = true")
	assert.True(t, realRunSeen, "真实注册阶段 ctx.DryRun 应为 false")
}

// TestDryRun_NormalRegistrationFalse 验证普通注册 ctx.DryRun == false
func TestDryRun_NormalRegistrationFalse(t *testing.T) {
	pm := NewManager(nil)

	var dryRunVal = true // 预设为 true，看 Setup 是否把它清成 false

	require.NoError(t, pm.Register(&Descriptor{
		Name: "normal-reg",
		Setup: func(ctx *SetupContext) (any, error) {
			dryRunVal = ctx.DryRun
			return nil, nil
		},
	}))

	assert.False(t, dryRunVal, "普通注册 ctx.DryRun 应为 false")
}

// TestDryRun_NoopEventBus_NoRealSubscription 验证 DryRun 阶段 EventBus 为 no-op，不产生真实订阅
func TestDryRun_NoopEventBus_NoRealSubscription(t *testing.T) {
	pm := NewManager(nil)

	var subTopic string

	desc := &Descriptor{
		Name: "eventbus-dryrun-test",
		Setup: func(ctx *SetupContext) (any, error) {
			if ctx.DryRun {
				// DryRun 阶段：Scope.Subscribe 内部使用 noopEventBus，订阅不产生真实效果
				sub, err := ctx.Scope().Subscribe("test.topic", func(_ context.Context, _ any) error { return nil })
				assert.NoError(t, err, "noopEventBus.Subscribe 不应返回错误")
				assert.NotNil(t, sub)
				subTopic = sub.Topic()
			}
			return nil, nil
		},
	}

	require.NoError(t, pm.RegisterMultipleSmart([]*Descriptor{desc}))
	assert.Equal(t, "test.topic", subTopic, "noopSubscription 应返回正确的 topic")

	// 确认真实 EventBus 上没有该订阅
	realStats := pm.eventBus.GetStats()
	assert.Equal(t, 0, realStats.SubscriptionCount, "DryRun 阶段不应在真实 EventBus 上注册订阅")
}

// TestDryRun_PluginCanSkipSideEffects 验证插件可以通过 ctx.DryRun 跳过外部副作用
func TestDryRun_PluginCanSkipSideEffects(t *testing.T) {
	pm := NewManager(nil)

	sideEffectCallCount := 0

	desc := &Descriptor{
		Name: "side-effect-guard",
		Setup: func(ctx *SetupContext) (any, error) {
			if !ctx.DryRun {
				// 仅在真实运行时执行副作用（如 HTTP 请求、全局变量写入）
				sideEffectCallCount++
			}
			return nil, nil
		},
	}

	// Smart 注册：DryRun 阶段不增加，真实注册阶段 +1
	require.NoError(t, pm.RegisterMultipleSmart([]*Descriptor{desc}))
	assert.Equal(t, 1, sideEffectCallCount, "副作用应仅在真实运行时执行一次")
}

// ===== L4: Service / ExportIface 测试 ==== (L3 testing helpers removed as MustAs/TryAs deprecated)
