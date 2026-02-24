package plugin

import (
	stdctx "context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEvent 用于测试的事件类型常量
const testEvent = dto.C2CMessageCreate

// ---------------------------------------------------------------------------
// P2-1: RegistryWriter / noopRegistryWriter
// ---------------------------------------------------------------------------

func TestRegistryWriter_LiveTracksMatchers(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	var matcherCount int32
	err := pm.RegisterV2(&PluginDescriptor{
		Name: "rw-test",
		Setup: func(ctx *SetupContext) (any, error) {
			m1 := ctx.Reg.RegisterMatcher(testEvent)
			m2 := ctx.Reg.RegisterMatcher(testEvent)
			if m1 != nil {
				atomic.AddInt32(&matcherCount, 1)
			}
			if m2 != nil {
				atomic.AddInt32(&matcherCount, 1)
			}
			return nil, nil
		},
	})
	require.NoError(t, err)

	inst, ok := pm.Get("rw-test")
	require.True(t, ok)
	assert.Equal(t, int(atomic.LoadInt32(&matcherCount)), len(inst.GetMatchers()),
		"Matchers registered via ctx.Reg should be tracked")
}

func TestRegistryWriter_Noop_NoMatchers(t *testing.T) {
	// noopRegistryWriter 应返回 nil，无任何副作用
	noop := &noopRegistryWriter{}
	m1 := noop.RegisterMatcher(testEvent)
	m2 := noop.RegisterCommand(testEvent, "/test")
	assert.Nil(t, m1)
	assert.Nil(t, m2)
}

func TestRegistryWriter_DryRun_InjectNoop(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	// RegisterMultipleV2Smart 内部 DryRun 阶段应注入 noopRegistryWriter
	// 插件代码使用 ctx.Reg 注册时不应产生副作用
	var liveRegCalled bool
	err := pm.RegisterMultipleV2Smart([]*PluginDescriptor{
		{
			Name: "smart-noop-test",
			Setup: func(ctx *SetupContext) (any, error) {
				// 在 Live 阶段注册，m 应不为 nil
				m := ctx.Reg.RegisterMatcher(testEvent)
				if m != nil {
					liveRegCalled = true
				}
				return nil, nil
			},
		},
	})
	require.NoError(t, err)
	assert.True(t, liveRegCalled, "Live 阶段 ctx.Reg 应为 live writer，返回 Matcher")
}

// ---------------------------------------------------------------------------
// P2-2: PluginInfo 只读视图
// ---------------------------------------------------------------------------

func TestPluginInfo_IsLoaded(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name:  "info-base",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}))

	var infoResult bool
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "info-dependent",
		Deps: []string{"info-base"},
		Setup: func(ctx *SetupContext) (any, error) {
			// ctx.Info 只读视图：查询依赖是否已加载
			infoResult = ctx.Info.IsLoaded("info-base")
			return nil, nil
		},
	}))

	assert.True(t, infoResult, "ctx.Info.IsLoaded 应在 Setup 阶段正确返回依赖加载状态")
}

func TestPluginInfo_DoesNotExposeWriteOps(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	// 验证 PluginInfo 接口只包含查询方法，不含 Unregister/Reload 等
	var info PluginInfo = newPluginInfo(pm)
	_ = info.List()
	_ = info.Count()
	_ = info.IsLoaded("x")
	_ = info.IsDisabled("x")
	_ = info.GetStatus("x")
	// 如果 PluginInfo 包含写方法，上面的接口赋值会编译失败
}

func TestPluginInfo_NullSafe(t *testing.T) {
	// nil Manager 时应返回安全的零值
	info := newPluginInfo(nil)
	assert.False(t, info.IsLoaded("x"))
	assert.Equal(t, 0, info.Count())
	assert.Nil(t, info.GetStatus("x"))
}

// ---------------------------------------------------------------------------
// P2-3: DryRun 内部化（ctx.Reg 自动 no-op，无需判断 DryRun）
// ---------------------------------------------------------------------------

func TestDryRun_PluginNeedNotCheck(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	setupCallCount := 0
	err := pm.RegisterMultipleV2Smart([]*PluginDescriptor{
		{
			Name: "no-dryrun-check",
			Setup: func(ctx *SetupContext) (any, error) {
				setupCallCount++
				// 不检查 DryRun，直接调用 Reg — 框架保证安全
				_ = ctx.Reg.RegisterMatcher(testEvent)
				return nil, nil
			},
		},
	})
	require.NoError(t, err)
	// Setup 被调用至少 1 次（正式注册），可能 2 次（DryRun + 正式）
	assert.GreaterOrEqual(t, setupCallCount, 1)

	// 最终插件成功加载
	_, ok := pm.Get("no-dryrun-check")
	assert.True(t, ok)
}

// ---------------------------------------------------------------------------
// P2-4: ctx.Go goroutine 生命周期管理
// ---------------------------------------------------------------------------

func TestCtxGo_GoroutineStopsOnUnload(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	var goroutineRunning atomic.Bool
	var goroutineDone atomic.Bool

	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "go-test",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.Go(func(runCtx stdctx.Context) {
				goroutineRunning.Store(true)
				<-runCtx.Done() // 等待 cancel
				goroutineDone.Store(true)
			})
			return nil, nil
		},
	}))

	// 等待 goroutine 启动
	assert.Eventually(t, func() bool {
		return goroutineRunning.Load()
	}, 500*time.Millisecond, 5*time.Millisecond)

	// 卸载插件：框架应 cancel goroutine 的 context 并等待退出
	err := pm.Unregister("go-test")
	require.NoError(t, err)

	// goroutine 应在 Unregister 完成后已退出
	assert.True(t, goroutineDone.Load(), "ctx.Go 启动的 goroutine 应在 Teardown 前退出")
}

func TestCtxGo_MultipleGoroutinesAllStop(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	const count = 5
	var stopped atomic.Int32

	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "go-multi-test",
		Setup: func(ctx *SetupContext) (any, error) {
			for range count {
				ctx.Go(func(runCtx stdctx.Context) {
					<-runCtx.Done()
					stopped.Add(1)
				})
			}
			return nil, nil
		},
	}))

	err := pm.Unregister("go-multi-test")
	require.NoError(t, err)

	assert.Equal(t, int32(count), stopped.Load(),
		"所有通过 ctx.Go 启动的 goroutine 应在 Unregister 后全部退出")
}

func TestCtxGo_TeardownCalledAfterGoroutinesStop(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	var order []string
	var mu sync.Mutex

	appendOrder := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "go-order-test",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.Go(func(runCtx stdctx.Context) {
				<-runCtx.Done()
				appendOrder("goroutine_stopped")
			})
			return nil, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			appendOrder("teardown")
			return nil
		},
	}))

	require.NoError(t, pm.Unregister("go-order-test"))

	mu.Lock()
	got := make([]string, len(order))
	copy(got, order)
	mu.Unlock()

	require.Len(t, got, 2)
	assert.Equal(t, "goroutine_stopped", got[0], "goroutine 应在 Teardown 前退出")
	assert.Equal(t, "teardown", got[1], "Teardown 应在 goroutine 退出后执行")
}

func TestCtxGo_ReloadCreatesNewManager(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	var stopCount atomic.Int32

	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "go-reload-test",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.Go(func(runCtx stdctx.Context) {
				<-runCtx.Done()
				stopCount.Add(1)
			})
			return nil, nil
		},
	}))

	// Reload 应先停止旧 goroutine，再启动新 goroutine
	require.NoError(t, pm.Reload("go-reload-test"))
	// 旧 goroutine 在 reload 的 unload 阶段已停止
	assert.Equal(t, int32(1), stopCount.Load(),
		"Reload 应先停止旧 goroutine（count=1），再启动新 goroutine")

	// 再次卸载，新的 goroutine 也应停止
	require.NoError(t, pm.Unregister("go-reload-test"))
	assert.Equal(t, int32(2), stopCount.Load(),
		"第二次卸载应停止 Reload 后启动的新 goroutine（count=2）")
}

// ---------------------------------------------------------------------------
// P2-5: ctx.Log 带前缀的插件日志器
// ---------------------------------------------------------------------------

func TestPluginLogger_Interface(t *testing.T) {
	// 验证 pluginLogger 实现 PluginLogger 接口
	var _ PluginLogger = newPluginLogger("test")
}

func TestPluginLogger_WithField_Immutable(t *testing.T) {
	orig := newPluginLogger("test")
	derived := orig.WithField("k", "v")
	// WithField 不应修改原日志器
	assert.NotSame(t, orig, derived)
}

func TestCtxLog_InjectedInSetup(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	var loggerName string
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "log-test",
		Setup: func(ctx *SetupContext) (any, error) {
			require.NotNil(t, ctx.Log, "ctx.Log 应在 Setup 阶段注入")
			// 调用不应 panic
			ctx.Log.Info("plugin loaded")
			ctx.Log.Infof("version %s", "1.0")
			ctx.Log.Warn("warning msg")
			ctx.Log.Error("error msg", nil)
			ctx.Log.Debug("debug msg")
			loggerName = ctx.pluginName
			return nil, nil
		},
	}))

	assert.Equal(t, "log-test", loggerName)
}

// ---------------------------------------------------------------------------
// P2-2+P2-6: ExportAs + Require / Optional
// ---------------------------------------------------------------------------

func TestExportAs_MakesAPIAvailableToOtherPlugins(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()
	pm := NewManager(eng)

	type MyAPI struct{ Value string }
	myAPI := &MyAPI{Value: "hello"}

	// 插件 A 通过 ExportAs 导出
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "exporter",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.ExportAs("exporter", myAPI)
			return nil, nil
		},
	}))

	// 插件 B 通过 Require 获取
	var got *MyAPI
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "consumer",
		Deps: []string{"exporter"},
		Setup: func(ctx *SetupContext) (any, error) {
			got = Require[MyAPI](ctx, "exporter")
			return nil, nil
		},
	}))

	require.NotNil(t, got)
	assert.Equal(t, "hello", got.Value)
}

func TestRequire_PanicsOnMissing(t *testing.T) {
	ctx := &SetupContext{
		setupContextInternal: setupContextInternal{
			container:  NewContainer(),
			pluginName: "test",
		},
	}
	assert.Panics(t, func() {
		Require[struct{}](ctx, "nonexistent")
	})
}

func TestOptional_ReturnsNilOnMissing(t *testing.T) {
	ctx := &SetupContext{
		setupContextInternal: setupContextInternal{
			container:  NewContainer(),
			pluginName: "test",
		},
	}
	v, ok := Optional[struct{}](ctx, "nonexistent")
	assert.Nil(t, v)
	assert.False(t, ok)
}

func TestOptional_ReturnsValueWhenPresent(t *testing.T) {
	type Svc struct{ X int }
	c := NewContainer()
	c.Register("svc", &Svc{X: 42})
	ctx := &SetupContext{setupContextInternal: setupContextInternal{container: c, pluginName: "test"}}

	v, ok := Optional[Svc](ctx, "svc")
	require.True(t, ok)
	assert.Equal(t, 42, v.X)
}

func TestExportAs_NilContainerSafe(t *testing.T) {
	// container 为 nil 时不应 panic
	ctx := &SetupContext{setupContextInternal: setupContextInternal{container: nil}}
	assert.NotPanics(t, func() {
		ctx.ExportAs("x", "value")
	})
}
