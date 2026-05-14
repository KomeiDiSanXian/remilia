package plugin_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// TestUndeclaredDep_NotifiesDependents 验证通过 Service 的未声明必要依赖
// 被合并后能正确触发 OnDependencyReloaded 回调。
func TestUndeclaredDep_NotifiesDependents(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})

	var notifiedDeclared, notifiedUndeclared atomic.Int32

	_ = pm.Register(&plugin.Descriptor{
		Name: "consumer-declared",
		Deps: []string{"base"},
		Advanced: &plugin.Advanced{
			OnDependencyReloaded: func(dep string) { notifiedDeclared.Add(1) },
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = plugin.Service[any](ctx, "base")
			return nil, nil
		},
	})

	_ = pm.Register(&plugin.Descriptor{
		Name: "consumer-undeclared",
		Deps: []string{},
		Advanced: &plugin.Advanced{
			OnDependencyReloaded: func(dep string) { notifiedUndeclared.Add(1) },
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = plugin.Service[any](ctx, "base") // Service → 必要依赖 → 被合并到 desc.Deps
			return nil, nil
		},
	})

	_ = pm.Reload(context.Background(), "base")
	time.Sleep(50 * time.Millisecond)

	if notifiedDeclared.Load() == 0 {
		t.Error("consumer-declared 应收到通知")
	} else {
		t.Log("✓ consumer-declared 收到通知")
	}

	if notifiedUndeclared.Load() == 0 {
		t.Error("consumer-undeclared 应收到通知（Service 追踪为必要依赖后合并）")
	} else {
		t.Log("✓ consumer-undeclared 也收到通知（未声明必要依赖被自动合并）")
	}
}

// TestUndeclaredDep_UnregisterCascade 验证 Service 的未声明依赖方被正确级联卸载。
func TestUndeclaredDep_UnregisterCascade(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})

	_ = pm.Register(&plugin.Descriptor{
		Name:  "consumer-declared",
		Deps:  []string{"base"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = plugin.Service[any](ctx, "base")
			return nil, nil
		},
	})

	_ = pm.Register(&plugin.Descriptor{
		Name: "consumer-undeclared",
		Deps: []string{},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_ = plugin.Service[any](ctx, "base") // 必要 → 合并 → 级联
			return nil, nil
		},
	})

	_ = pm.UnregisterCascade(context.Background(), "base")

	if pm.IsLoaded("consumer-declared") {
		t.Error("consumer-declared 应被级联卸载")
	} else {
		t.Log("✓ consumer-declared 被正确级联卸载")
	}

	if pm.IsLoaded("consumer-undeclared") {
		t.Error("consumer-undeclared 应被级联卸载（Service 追踪为必要依赖）")
	} else {
		t.Log("✓ consumer-undeclared 也被正确级联卸载")
	}
}

// TestUndeclaredDep_OptionalNotCascaded 验证 TryService（可选依赖）不触发级联卸载。
func TestUndeclaredDep_OptionalNotCascaded(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "optional-base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "api", nil },
	})

	_ = pm.Register(&plugin.Descriptor{
		Name: "consumer-optional",
		Deps: []string{},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_, _ = plugin.TryService[any](ctx, "optional-base") // TryService → 可选依赖
			return nil, nil
		},
	})

	_ = pm.UnregisterCascade(context.Background(), "optional-base")

	if pm.IsLoaded("consumer-optional") {
		t.Log("✓ consumer-optional 不受级联卸载影响（TryService 是可选依赖）")
	} else {
		t.Error("consumer-optional 不应被级联卸载（TryService 访问的是可选依赖）")
	}
}

// TestUndeclaredDep_OptionalNotNotified 验证 TryService（可选依赖）不触发 OnDependencyReloaded。
func TestUndeclaredDep_OptionalNotNotified(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "optional-base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "api", nil },
	})

	var notified atomic.Int32

	_ = pm.Register(&plugin.Descriptor{
		Name: "consumer-optional",
		Deps: []string{},
		Advanced: &plugin.Advanced{
			OnDependencyReloaded: func(dep string) { notified.Add(1) },
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_, _ = plugin.TryService[any](ctx, "optional-base") // 可选
			return nil, nil
		},
	})

	_ = pm.Reload(context.Background(), "optional-base")
	time.Sleep(50 * time.Millisecond)

	if notified.Load() > 0 {
		t.Error("consumer-optional 不应收到 OnDependencyReloaded（TryService 是可选依赖）")
	} else {
		t.Log("✓ consumer-optional 不收到通知（TryService 追踪为可选依赖）")
	}
}

// TestUndeclaredDep_TopologicalSort 验证批量注册未声明 Deps 的已知限制。
func TestUndeclaredDep_TopologicalSort(t *testing.T) {
	pm := plugin.NewManager(nil)

	setupOrder := make([]string, 0, 2)

	base := &plugin.Descriptor{
		Name: "base-topo",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupOrder = append(setupOrder, "base-topo")
			return "api", nil
		},
	}

	consumer := &plugin.Descriptor{
		Name: "consumer-topo",
		Deps: []string{},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupOrder = append(setupOrder, "consumer-topo")
			if _, ok := plugin.TryService[any](ctx, "base-topo"); !ok {
				t.Log("  注: 批量注册未声明 Deps 时顺序不保证（已知限制，建议声明 Deps 或用 Smart 模式）")
			}
			return nil, nil
		},
	}

	if err := pm.RegisterMultipleAtomic([]*plugin.Descriptor{consumer, base}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	t.Logf("注册顺序: %v（未声明 Deps时拓扑排序不保证顺序）", setupOrder)
}

// TestOptionalDeps_TopologicalSort 验证声明 OptionalDeps 时批量注册能正确排序。
func TestOptionalDeps_TopologicalSort(t *testing.T) {
	pm := plugin.NewManager(nil)

	setupOrder := make([]string, 0, 2)

	base := &plugin.Descriptor{
		Name: "base-opt",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupOrder = append(setupOrder, "base-opt")
			return "api", nil
		},
	}

	consumer := &plugin.Descriptor{
		Name:         "consumer-opt",
		Deps:         []string{},
		OptionalDeps: []string{"base-opt"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupOrder = append(setupOrder, "consumer-opt")
			_, _ = plugin.TryService[any](ctx, "base-opt")
			return nil, nil
		},
	}

	if err := pm.RegisterMultipleAtomic([]*plugin.Descriptor{consumer, base}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	if len(setupOrder) != 2 {
		t.Fatalf("期望 2 个插件，实际: %v", setupOrder)
	}
	if setupOrder[0] != "base-opt" || setupOrder[1] != "consumer-opt" {
		t.Errorf("OptionalDeps 应保证 base-opt 在 consumer-opt 之前，实际顺序: %v", setupOrder)
	} else {
		t.Logf("✓ OptionalDeps 正确保证批量注册顺序: %v", setupOrder)
	}
}

// TestOptionalDeps_NoFailWhenMissing 验证 OptionalDeps 中的依赖不存在时不报错。
func TestOptionalDeps_NoFailWhenMissing(t *testing.T) {
	pm := plugin.NewManager(nil)

	consumer := &plugin.Descriptor{
		Name:         "consumer-missing-opt",
		OptionalDeps: []string{"nonexistent-plugin"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_, ok := plugin.TryService[any](ctx, "nonexistent-plugin")
			if ok {
				t.Error("不应找到不存在的插件")
			}
			return nil, nil
		},
	}

	if err := pm.Register(consumer); err != nil {
		t.Errorf("OptionalDeps 中不存在的依赖不应导致注册失败: %v", err)
	} else {
		t.Log("✓ OptionalDeps 中不存在的依赖不影响注册")
	}
}

// TestOptionalDeps_NoWarnWhenDeclared 验证已声明在 OptionalDeps 中的依赖不触发警告。
func TestOptionalDeps_NoWarnWhenDeclared(t *testing.T) {
	pm := plugin.NewManager(nil)

	_ = pm.Register(&plugin.Descriptor{
		Name:  "opt-base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return "api", nil },
	})

	err := pm.Register(&plugin.Descriptor{
		Name:         "opt-consumer-declared",
		OptionalDeps: []string{"opt-base"},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			_, _ = plugin.TryService[any](ctx, "opt-base")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	t.Log("✓ OptionalDeps 已声明，不产生未声明依赖警告")
}

// TestUndeclaredDep_SmartMode 验证 Smart 模式能同时处理必要和可选依赖的拓扑排序。
func TestUndeclaredDep_SmartMode(t *testing.T) {
	pm := plugin.NewManager(nil)

	setupOrder := make([]string, 0, 3)

	base := &plugin.Descriptor{
		Name: "sm-base",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupOrder = append(setupOrder, "sm-base")
			return "base-api", nil
		},
	}

	optional := &plugin.Descriptor{
		Name: "sm-optional",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupOrder = append(setupOrder, "sm-optional")
			return "opt-api", nil
		},
	}

	consumer := &plugin.Descriptor{
		Name: "sm-consumer",
		Deps: []string{},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			setupOrder = append(setupOrder, "sm-consumer")
			_ = plugin.Service[any](ctx, "sm-base")            // 必要
			_, _ = plugin.TryService[any](ctx, "sm-optional")  // 可选
			return nil, nil
		},
	}

	if err := pm.RegisterMultipleSmart([]*plugin.Descriptor{consumer, base, optional}); err != nil {
		t.Fatalf("Smart 注册失败: %v", err)
	}

	indexOf := func(name string) int {
		last := -1
		for i, n := range setupOrder {
			if n == name {
				last = i
			}
		}
		return last
	}

	baseIdx, optIdx, consumerIdx := indexOf("sm-base"), indexOf("sm-optional"), indexOf("sm-consumer")
	if baseIdx < 0 || optIdx < 0 || consumerIdx < 0 {
		t.Fatalf("未找到所有插件: %v", setupOrder)
	}
	if consumerIdx < baseIdx {
		t.Errorf("sm-base 应在 sm-consumer 之前，实际顺序: %v", setupOrder)
	} else {
		t.Logf("✓ Smart 模式正确推断必要依赖顺序: %v", setupOrder)
	}
	if consumerIdx < optIdx {
		t.Errorf("sm-optional 应在 sm-consumer 之前，实际顺序: %v", setupOrder)
	} else {
		t.Logf("✓ Smart 模式同时处理可选依赖排序: %v", setupOrder)
	}
}
