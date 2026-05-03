package plugin

import (
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
				// DryRun 阶段：EventBus 是 noopEventBus，订阅不产生真实效果
				sub, err := ctx.EventBus.Subscribe("test.topic", func(_ any) {})
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

// ===== L4: MustAs / TryAs / ExportInterface 测试 =====

// testClientIface 用于测试的接口
type testClientIface interface {
	GetValue() string
}

// testClientImpl 实现 testClientIface
type testClientImpl struct{ value string }

func (c *testClientImpl) GetValue() string { return c.value }

// TestMustAs_Success 验证 MustAs 能正确获取接口类型依赖
func TestMustAs_Success(t *testing.T) {
	pm := NewManager(nil)

	impl := &testClientImpl{value: "hello"}

	require.NoError(t, pm.Register(&Descriptor{
		Name: "provider-mustas",
		Setup: func(ctx *SetupContext) (any, error) {
			ExportIface[testClientIface](ctx, "test.client", impl)
			return impl, nil
		},
	}))

	var got testClientIface
	require.NoError(t, pm.Register(&Descriptor{
		Name: "consumer-mustas",
		Setup: func(ctx *SetupContext) (any, error) {
			got = MustAs[testClientIface](ctx, "test.client")
			return nil, nil
		},
	}))

	require.NotNil(t, got)
	assert.Equal(t, "hello", got.GetValue())
}

// TestMustAs_Panic_NotFound 验证 MustAs 在依赖不存在时 panic
func TestMustAs_Panic_NotFound(t *testing.T) {
	pm := NewManager(nil)

	require.NoError(t, pm.Register(&Descriptor{
		Name: "panic-mustas",
		Setup: func(ctx *SetupContext) (any, error) {
			assert.Panics(t, func() {
				_ = MustAs[testClientIface](ctx, "nonexistent")
			})
			return nil, nil
		},
	}))
}

// TestMustAs_Panic_WrongType 验证 MustAs 在类型不匹配时 panic
func TestMustAs_Panic_WrongType(t *testing.T) {
	pm := NewManager(nil)

	// 先注册一个不实现 testClientIface 的插件
	require.NoError(t, pm.Register(&Descriptor{
		Name: "wrong-type-provider",
		Setup: func(ctx *SetupContext) (any, error) {
			return "just a string", nil
		},
	}))

	require.NoError(t, pm.Register(&Descriptor{
		Name: "wrong-type-consumer",
		Setup: func(ctx *SetupContext) (any, error) {
			assert.Panics(t, func() {
				_ = MustAs[testClientIface](ctx, "wrong-type-provider")
			})
			return nil, nil
		},
	}))
}

// TestTryAs_Success 验证 TryAs 能正确获取接口类型可选依赖
func TestTryAs_Success(t *testing.T) {
	pm := NewManager(nil)

	impl := &testClientImpl{value: "world"}

	require.NoError(t, pm.Register(&Descriptor{
		Name: "provider-tryas",
		Setup: func(ctx *SetupContext) (any, error) {
			ExportIface[testClientIface](ctx, "test.client.opt", impl)
			return impl, nil
		},
	}))

	var got testClientIface
	var found bool
	require.NoError(t, pm.Register(&Descriptor{
		Name: "consumer-tryas",
		Setup: func(ctx *SetupContext) (any, error) {
			got, found = TryAs[testClientIface](ctx, "test.client.opt")
			return nil, nil
		},
	}))

	assert.True(t, found)
	require.NotNil(t, got)
	assert.Equal(t, "world", got.GetValue())
}

// TestTryAs_NotFound 验证 TryAs 在依赖不存在时返回零值和 false
func TestTryAs_NotFound(t *testing.T) {
	pm := NewManager(nil)

	require.NoError(t, pm.Register(&Descriptor{
		Name: "tryas-notfound",
		Setup: func(ctx *SetupContext) (any, error) {
			got, ok := TryAs[testClientIface](ctx, "nonexistent")
			assert.False(t, ok)
			assert.Nil(t, got)
			return nil, nil
		},
	}))
}

// TestTryAs_WrongType 验证 TryAs 在类型不匹配时返回 false
func TestTryAs_WrongType(t *testing.T) {
	pm := NewManager(nil)

	require.NoError(t, pm.Register(&Descriptor{
		Name: "tryas-wrong-provider",
		Setup: func(ctx *SetupContext) (any, error) {
			return "just a string", nil
		},
	}))

	require.NoError(t, pm.Register(&Descriptor{
		Name: "tryas-wrong-consumer",
		Setup: func(ctx *SetupContext) (any, error) {
			got, ok := TryAs[testClientIface](ctx, "tryas-wrong-provider")
			assert.False(t, ok, "类型不匹配时 TryAs 应返回 false")
			assert.Zero(t, got)
			return nil, nil
		},
	}))
}

// ===== Multi-interface export test helpers (package-level) =====

type testWriterIface interface{ Write() }
type testReaderIface interface{ Read() string }

type testFullImpl struct{ data string }

func (f *testFullImpl) Write()       {}
func (f *testFullImpl) Read() string { return f.data }

// TestExportInterface_MultipleKeys 验证同一插件可以以多个接口 key 导出
func TestExportInterface_MultipleKeys(t *testing.T) {
	pm := NewManager(nil)

	impl := &testFullImpl{data: "test-data"}

	require.NoError(t, pm.Register(&Descriptor{
		Name: "multi-interface-provider",
		Setup: func(ctx *SetupContext) (any, error) {
			ExportIface[testWriterIface](ctx, "multi.writer", impl)
			ExportIface[testReaderIface](ctx, "multi.reader", impl)
			return impl, nil
		},
	}))

	var w testWriterIface
	var r testReaderIface
	require.NoError(t, pm.Register(&Descriptor{
		Name: "multi-interface-consumer",
		Setup: func(ctx *SetupContext) (any, error) {
			w = MustAs[testWriterIface](ctx, "multi.writer")
			r = MustAs[testReaderIface](ctx, "multi.reader")
			return nil, nil
		},
	}))

	assert.NotNil(t, w)
	assert.NotNil(t, r)
	assert.Equal(t, "test-data", r.Read())
}
