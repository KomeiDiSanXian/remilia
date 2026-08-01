package plugin

// design_review_fixes_test.go — 插件设计评审修复回归测试
//
// 覆盖：
//   - H2: InPlace 重载后 Spawn 的 goroutine 仍然存活（goroutineMgr 继承）
//   - H3: 并发 StartAll 不会导致二次 Setup（状态转换原子化）
//   - M1: StopAll 后 StartAll 可重启（metaGM 可重建）
//   - M2: BlueGreen 重载补 RestoreState
//   - M5: BlueGreen 旧实例 Teardown 看到旧 Config

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReloadInPlace_SpawnSurvives 验证 InPlace 重载后通过 ctx.Spawn 启动的
// goroutine 仍然存活且可继续执行（此前 newContext 未继承 goroutineMgr，
// Spawn 静默 no-op，重载后后台任务整体丢失）。
func TestReloadInPlace_SpawnSurvives(t *testing.T) {
	pm := NewManager(nil)

	var ticks atomic.Int64
	desc := &Descriptor{
		Name: "inplace-spawn",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil
		},
		Advanced: &Advanced{
			Strategy: ReloadInPlace,
			Reload: func(ctx *SetupContext) error {
				ctx.Spawn(func(ctx context.Context) {
					for {
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Millisecond):
							ticks.Add(1)
						}
					}
				})
				return nil
			},
		},
	}
	require.NoError(t, pm.Register(desc))

	require.NoError(t, pm.Reload(context.Background(), "inplace-spawn"))

	deadline := time.Now().Add(2 * time.Second)
	for ticks.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	assert.Greater(t, ticks.Load(), int64(0), "InPlace 重载后 Spawn 的 goroutine 应存活并持续执行")
}

// TestStartAll_Concurrent_NoDoubleSetup 验证并发 StartAll 不会导致二次 Setup。
func TestStartAll_Concurrent_NoDoubleSetup(t *testing.T) {
	pm := NewManager(nil)

	const n = 3
	counters := make([]*atomic.Int64, n)
	for i := range n {
		counters[i] = &atomic.Int64{}
		idx := i
		require.NoError(t, pm.Register(&Descriptor{
			Name: "startall-" + string(rune('a'+idx)),
			Setup: func(ctx *SetupContext) (any, error) {
				counters[idx].Add(1)
				return nil, nil
			},
		}))
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			_ = pm.StartAll(context.Background())
		})
	}
	wg.Wait()

	for i, c := range counters {
		assert.Equal(t, int64(1), c.Load(), "plugin %d 的 Setup 必须只执行一次", i)
	}
}

// TestStopAll_ThenStartAll_Restarts 验证 StopAll 后 Manager 仍可 StartAll
// （metaGM 重建），插件重新加载。
func TestStopAll_ThenStartAll_Restarts(t *testing.T) {
	pm := NewManager(nil)

	var setupCount atomic.Int64
	require.NoError(t, pm.Register(&Descriptor{
		Name: "restartable",
		Setup: func(ctx *SetupContext) (any, error) {
			setupCount.Add(1)
			return nil, nil
		},
	}))

	require.NoError(t, pm.StartAll(context.Background()))
	require.NoError(t, pm.StopAll(context.Background()))

	// StopAll 后 StartAll：插件应重新加载
	require.NoError(t, pm.StartAll(context.Background()))
	assert.True(t, pm.IsLoaded("restartable"))
	assert.Equal(t, int64(2), setupCount.Load(), "Stop→Start 循环应触发二次 Setup")

	require.NoError(t, pm.StopAll(context.Background()))
}

// TestReloadBlueGreen_RestoreStateCalled 验证 BlueGreen 重载补 RestoreState
// （此前 SaveState 成果被白白丢弃，MigrateState 永不生效）。
func TestReloadBlueGreen_RestoreStateCalled(t *testing.T) {
	pm := NewManager(nil)

	var restoreCalled atomic.Bool
	require.NoError(t, pm.Register(&Descriptor{
		Name: "bg-restore",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil
		},
		Advanced: &Advanced{
			Strategy: ReloadBlueGreen,
			SaveState: func() (any, error) {
				return "saved-state", nil
			},
			RestoreState: func(state any) error {
				restoreCalled.Store(state == "saved-state")
				return nil
			},
		},
	}))

	require.NoError(t, pm.Reload(context.Background(), "bg-restore"))
	assert.True(t, restoreCalled.Load(), "BlueGreen 重载后应调用 RestoreState 恢复保存的状态")

	// 等待异步旧实例清理完成，避免 goroutine 泄漏
	pm.stats.drainDrainingInstances(context.Background())
	_ = pm.StopAll(context.Background())
}

// TestReloadBlueGreen_OldTeardownSeesOldConfig 验证 BG 旧实例 Teardown 收到
// 的是旧 Config（此前混用新实例替换过的 Config）。
func TestReloadBlueGreen_OldTeardownSeesOldConfig(t *testing.T) {
	stubProvider := &stubConfigProvider{subs: map[string]map[string]any{"bg-teardown-cfg": {"k": "v"}}}
	pm := NewManager(nil, WithConfigProvider(stubProvider))

	var (
		firstSetupCfg any
		teardownCfg   any
		cfgMu         sync.Mutex
		setupRuns     int
	)

	require.NoError(t, pm.Register(&Descriptor{
		Name: "bg-teardown-cfg",
		Setup: func(ctx *SetupContext) (any, error) {
			cfgMu.Lock()
			setupRuns++
			if setupRuns == 1 {
				firstSetupCfg = ctx.Config
			}
			cfgMu.Unlock()
			// 第二次 Setup（新实例）替换 Config 为 nil，模拟插件更新配置对象；
			// 首次 Setup 不得改动 ctx.Config——oldContext 就是首个 SetupContext，
			// 其 Config 必须保持旧值供旧实例 Teardown 断言。
			if setupRuns > 1 {
				ctx.Config = nil
			}
			return nil, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			cfgMu.Lock()
			// 只记录第一次（BG 旧实例的）Teardown；StopAll 会对新实例再次调用
			if teardownCfg == nil {
				teardownCfg = ctx.Config
			}
			cfgMu.Unlock()
			return nil
		},
		Advanced: &Advanced{
			Strategy: ReloadBlueGreen,
		},
	}))

	require.NoError(t, pm.Reload(context.Background(), "bg-teardown-cfg"))
	t.Logf("draining after reload: %v", pm.stats.ListDraining())

	// 等待异步旧实例清理（Teardown 在后台 goroutine 执行）
	pm.stats.drainDrainingInstances(context.Background())
	// 先停止再断言，避免 Teardown 与断言争抢 cfgMu
	_ = pm.StopAll(context.Background())

	cfgMu.Lock()
	defer cfgMu.Unlock()
	require.NotNil(t, firstSetupCfg, "有配置提供者时首次 Setup 的 Config 应为非 nil")
	assert.Equal(t, firstSetupCfg, teardownCfg,
		"旧实例 Teardown 应看到旧 Config 对象，而非新实例替换后的 nil")
}

// TestBatch_DependencyAwareRetry 验证未声明依赖（非 DryRunSafe）的插件在
// 批内依赖顺序错误时自动修复：consumer 排 base 之前且漏写 Deps，
// 依赖感知重试会在 base 注册后自动重新注册 consumer，整批不再失败。
func TestBatch_DependencyAwareRetry(t *testing.T) {
	pm := NewManager(nil)

	setupOrder := make([]string, 0, 3)
	var orderMu sync.Mutex

	consumer := &Descriptor{
		Name: "retry-consumer", // 故意不写 Deps，也不声明 DryRunSafe
		Setup: func(ctx *SetupContext) (any, error) {
			orderMu.Lock()
			setupOrder = append(setupOrder, "consumer")
			orderMu.Unlock()
			_ = Service[any](ctx, "retry-base")
			return nil, nil
		},
	}
	base := &Descriptor{
		Name: "retry-base",
		Setup: func(ctx *SetupContext) (any, error) {
			orderMu.Lock()
			setupOrder = append(setupOrder, "base")
			orderMu.Unlock()
			return "base-api", nil
		},
	}

	// consumer 在 base 之前 → 首轮失败进 pending，重试轮自动修复
	require.NoError(t, pm.RegisterBatch(context.Background(), []*Descriptor{consumer, base}))

	assert.True(t, pm.IsLoaded("retry-consumer"))
	assert.True(t, pm.IsLoaded("retry-base"))

	orderMu.Lock()
	defer orderMu.Unlock()
	baseIdx := -1
	consumerIdx := -1
	for i, n := range setupOrder {
		switch n {
		case "base":
			baseIdx = i
		case "consumer":
			consumerIdx = i
		}
	}
	require.NotEqual(t, -1, baseIdx, "base 应至少 Setup 一次")
	require.NotEqual(t, -1, consumerIdx, "consumer 应至少 Setup 一次")
	assert.Less(t, baseIdx, consumerIdx, "最终顺序应为 base 先于 consumer")

	// 依赖合并且已声明：消费方 Deps 中应包含 base（trackedDeps 合并）
	inst, ok := pm.Get("retry-consumer")
	require.True(t, ok)
	assert.Contains(t, inst.desc.Deps, "retry-base")
}

// TestBatch_MissingDependency_ReportsActionableError 验证依赖确实缺失时
// 返回可操作的错误信息，且不影响本批其他插件的注册。
func TestBatch_MissingDependency_ReportsActionableError(t *testing.T) {
	pm := NewManager(nil)

	consumer := &Descriptor{
		Name: "missing-dep-consumer",
		Setup: func(ctx *SetupContext) (any, error) {
			_ = Service[any](ctx, "ghost-plugin")
			return nil, nil
		},
	}
	independent := &Descriptor{
		Name:  "independent-plugin",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}

	err := pm.RegisterBatch(context.Background(), []*Descriptor{consumer, independent})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependencies", "错误信息应指明依赖缺失")
	assert.Contains(t, err.Error(), "Deps", "错误信息应给出声明 Deps 的补救提示")

	// 独立插件不受影响
	assert.True(t, pm.IsLoaded("independent-plugin"))
	assert.False(t, pm.IsLoaded("missing-dep-consumer"))
}

type stubConfigProvider struct {
	subs map[string]map[string]any
}

func (s *stubConfigProvider) Sub(pluginName string) map[string]any {
	if s == nil || s.subs == nil {
		return nil
	}
	return s.subs[pluginName]
}

func (s *stubConfigProvider) OnConfigChange(callback func()) {}
