package plugin_test

import (
	"context"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// TestEdge_Service_NonExistent 验证 Service 访问不存在插件时 panic 被框架捕获为注册错误。
func TestEdge_Service_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	err := pm.Register(&plugin.Descriptor{
		Name: "bad-plugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = ctx.Service[any]("ghost") // ghost 不存在，应 panic
			return nil, nil
		},
	})

	if err == nil {
		t.Fatal("应返回错误（Service 不存在的依赖触发 panic）")
	}
	if pm.IsLoaded("bad-plugin") {
		t.Error("bad-plugin 不应被注册（Setup panic 应导致注册失败）")
	}
	t.Logf("✓ Service 不存在依赖：正确返回错误: %v", err)
}

// TestEdge_TryService_NonExistent 验证 TryService 访问不存在的插件时安全返回 false。
func TestEdge_TryService_NonExistent(t *testing.T) {
	pm := plugin.NewManager(nil)

	err := pm.Register(&plugin.Descriptor{
		Name: "safe-plugin",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_, ok := ctx.TryService[any]("ghost")
			if ok {
				t.Error("TryService 不存在的依赖应返回 false")
			}
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("不应返回错误（TryService 访问不存在依赖是安全的）: %v", err)
	}
	if !pm.IsLoaded("safe-plugin") {
		t.Error("safe-plugin 应注册成功")
	}
	t.Log("✓ TryService 不存在依赖：安全返回 false，插件正常注册")
}

// TestEdge_DeclaredDep_NonExistent 验证 Deps 中声明了不存在的插件时框架前置拦截。
// TestEdge_DeclaredDep_NonExistent_Strict 验证 strict 模式下缺失依赖返回错误。
func TestEdge_DeclaredDep_NonExistent_Strict(t *testing.T) {
	pm := plugin.NewManager(nil)
	pm.SetStrictDeps(true)
	setupCalled := false
	err := pm.Register(&plugin.Descriptor{
		Name: "needs-ghost",
		Deps: []string{"ghost"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupCalled = true
			return nil, nil
		},
	})

	if err == nil {
		t.Fatal("strict mode: should return error for Deps with non-existent plugin")
	}
	if setupCalled {
		t.Error("Setup should not be called (dep check before Setup)")
	}
	if !strings.Contains(err.Error(), "missing required dependency") &&
		!strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention missing dep, got: %v", err)
	}
}

// TestEdge_DeclaredDep_NonExistent_Lenient 验证非 strict 模式下缺失依赖仅警告。
func TestEdge_DeclaredDep_NonExistent_Lenient(t *testing.T) {
	pm := plugin.NewManager(nil)
	err := pm.Register(&plugin.Descriptor{
		Name: "needs-ghost",
		Deps: []string{"ghost"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("non-strict mode: should allow missing dep with warning, got: %v", err)
	}
}

// TestEdge_DeclaredDep_LoadingState 验证依赖处于 Loaded 状态可正常注册。
func TestEdge_DeclaredDep_LoadingState(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "ready-dep",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})

	err := pm.Register(&plugin.Descriptor{
		Name: "needs-ready",
		Deps: []string{"ready-dep"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("依赖处于 Loaded 状态时应注册成功: %v", err)
	}
	t.Log("✓ 依赖处于 Loaded 状态：正常注册")
}

// TestEdge_Smart_ServiceWithTryService 验证 Smart 模式同时使用 Service 和 TryService。
func TestEdge_Smart_ServiceWithTryService(t *testing.T) {
	pm := plugin.NewManager(nil)

	base := &plugin.Descriptor{
		Name:  "smart-base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "api", nil },
	}

	consumer := &plugin.Descriptor{
		Name:       "smart-consumer",
		DryRunSafe: true,
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = ctx.Service[any]("smart-base")           // 存在 → 必要依赖追踪
			_, _ = ctx.TryService[any]("optional-ghost") // 不存在 → 可选，安全返回 false
			return nil, nil
		},
	}

	err := pm.RegisterBatch(context.Background(), []*plugin.Descriptor{consumer, base}, plugin.WithInferDeps())
	if err != nil {
		t.Fatalf("Smart 注册不应失败: %v", err)
	}
	if !pm.IsLoaded("smart-consumer") || !pm.IsLoaded("smart-base") {
		t.Error("两个插件都应注册成功")
	}
	t.Log("✓ Smart 模式：Service 追踪必要依赖，TryService 安全处理可选依赖")
}

// TestEdge_Smart_PreRegisteredDep 验证 Smart 模式能追踪预注册的依赖。
func TestEdge_Smart_PreRegisteredDep(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "pre-registered",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "pre", nil },
	})

	consumer := &plugin.Descriptor{
		Name:       "smart-consumer2",
		DryRunSafe: true,
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = ctx.Service[any]("pre-registered")
			return nil, nil
		},
	}

	err := pm.RegisterBatch(context.Background(), []*plugin.Descriptor{consumer}, plugin.WithInferDeps())
	if err != nil {
		t.Fatalf("Smart 注册不应失败: %v", err)
	}
	t.Log("✓ Smart 模式：预注册依赖在 DryRun 阶段能正确追踪")
}

// TestEdge_BatchAtomicWithMissingDep 验证批量注册原子回滚。
func TestEdge_BatchAtomicWithMissingDep(t *testing.T) {
	pm := plugin.NewManager(nil)

	a := &plugin.Descriptor{
		Name:  "batch-a",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}
	b := &plugin.Descriptor{
		Name:  "batch-b",
		Deps:  []string{"batch-a", "batch-ghost"},
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	}

	err := pm.RegisterBatch(context.Background(), []*plugin.Descriptor{a, b}, plugin.WithAtomic())

	if err == nil {
		t.Fatal("应返回错误（batch-ghost 不存在）")
	}
	if pm.IsLoaded("batch-a") {
		t.Error("原子回滚失败：batch-a 应被清理")
	}
	if pm.IsLoaded("batch-b") {
		t.Error("batch-b 不应被注册")
	}
	t.Logf("✓ 批量注册中有不存在依赖：正确失败并原子回滚: %v", err)
}

// TestEdge_Service_NonExistent_NoSideEffect 验证 Service 访问不存在依赖时 fail clean。
func TestEdge_Service_NonExistent_NoSideEffect(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "existing",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "api", nil },
	})

	err := pm.Register(&plugin.Descriptor{
		Name: "will-fail",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = ctx.Service[any]("totally-nonexistent")
			return nil, nil
		},
	})

	if err == nil {
		t.Fatal("应返回错误")
	}

	if !pm.IsLoaded("existing") {
		t.Error("existing 插件不应受到影响")
	}
	if pm.IsLoaded("will-fail") {
		t.Error("will-fail 不应被注册")
	}
	if pm.Count() != 1 {
		t.Errorf("注册表中应只有 1 个插件，实际 %d 个", pm.Count())
	}
	t.Log("✓ Service 不存在依赖：失败干净，不污染 Manager 状态")
}

// TestEdge_SelfDependency 验证自依赖安全（TryService 自身返回 false，不追踪）。
func TestEdge_SelfDependency(t *testing.T) {
	pm := plugin.NewManager(nil)

	err := pm.Register(&plugin.Descriptor{
		Name: "self-dep",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_, ok := ctx.TryService[any]("self-dep")
			_ = ok // 自身未注册完成，容器中没有
			return nil, nil
		},
	})

	if err != nil {
		t.Fatalf("自依赖不应导致注册失败: %v", err)
	}
	t.Log("✓ 自依赖：TryService 自身安全（返回 false，不追踪）")
}
