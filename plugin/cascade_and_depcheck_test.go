package plugin

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// newTestManager 创建用于测试的 Manager（无 engine）
func newTestManager() *Manager {
	return NewManager(nil)
}

// makeSimpleDescriptor 创建一个最简单的插件描述符（无副作用的 Setup）
func makeSimpleDescriptor(name string, deps []string) *PluginDescriptor {
	return &PluginDescriptor{
		Name:  name,
		Deps:  deps,
		Setup: func(ctx *SetupContext) error { return nil },
	}
}

// ---- UnregisterCascade 测试 ------------------------------------------------

func TestUnregisterCascade_NoDependents(t *testing.T) {
	pm := NewManager(newCoordinator())
	pm.RegisterV2(makeSimpleDescriptor("A", nil))

	if err := pm.UnregisterCascade("A"); err != nil {
		t.Fatalf("UnregisterCascade single plugin: %v", err)
	}
	if pm.IsLoaded("A") {
		t.Error("plugin A should be unregistered")
	}
}

func TestUnregisterCascade_WithDependents(t *testing.T) {
	pm := NewManager(newCoordinator())
	pm.RegisterV2(makeSimpleDescriptor("base", nil))
	pm.RegisterV2(makeSimpleDescriptor("mid", []string{"base"}))
	pm.RegisterV2(makeSimpleDescriptor("top", []string{"mid"}))

	// 级联卸载 base，应同时卸载 mid 和 top
	if err := pm.UnregisterCascade("base"); err != nil {
		t.Fatalf("UnregisterCascade: %v", err)
	}
	for _, name := range []string{"base", "mid", "top"} {
		if pm.IsLoaded(name) {
			t.Errorf("plugin %s should be unregistered after cascade", name)
		}
	}
}

func TestUnregisterCascade_PartialTree(t *testing.T) {
	pm := NewManager(newCoordinator())
	pm.RegisterV2(makeSimpleDescriptor("A", nil))
	pm.RegisterV2(makeSimpleDescriptor("B", nil))
	pm.RegisterV2(makeSimpleDescriptor("C", []string{"A"}))

	// 卸载 A 应卸载 A 和 C，但 B 不受影响
	if err := pm.UnregisterCascade("A"); err != nil {
		t.Fatalf("UnregisterCascade: %v", err)
	}
	if pm.IsLoaded("A") {
		t.Error("plugin A should be unregistered")
	}
	if pm.IsLoaded("C") {
		t.Error("plugin C should be unregistered (depends on A)")
	}
	if !pm.IsLoaded("B") {
		t.Error("plugin B should still be loaded (independent)")
	}
}

func TestUnregisterCascade_NotFound(t *testing.T) {
	pm := NewManager(newCoordinator())
	err := pm.UnregisterCascade("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

// ---- 依赖 Loaded 状态检查测试 -----------------------------------------------

func TestRegisterV2_DependencyMustBeLoaded(t *testing.T) {
	pm := NewManager(newCoordinator())

	// 注册 A（成功）
	pm.RegisterV2(makeSimpleDescriptor("A", nil))

	// 注册 B 依赖 A（A 已 Loaded，应成功）
	if err := pm.RegisterV2(makeSimpleDescriptor("B", []string{"A"})); err != nil {
		t.Fatalf("B should register successfully when A is Loaded: %v", err)
	}
}

func TestRegisterV2_DependencyLoading_ShouldFail(t *testing.T) {
	pm := NewManager(newCoordinator())

	// 人为构造一个 "Loading" 状态的假插件放入 pm.plugins
	fakeInst := &PluginInstance{
		desc:     &PluginDescriptor{Name: "fake-loading"},
		state:    Loading,
		matchers: nil,
	}
	pm.mu.Lock()
	pm.plugins["fake-loading"] = fakeInst
	pm.mu.Unlock()

	// 尝试注册依赖 fake-loading 的插件，应失败（fake-loading 处于 Loading 状态）
	err := pm.RegisterV2(makeSimpleDescriptor("dependent", []string{"fake-loading"}))
	if err == nil {
		t.Fatal("expected error when dependency is in Loading state")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Errorf("expected 'not ready' error, got: %v", err)
	}
}

func TestRegisterV2_MissingDependency(t *testing.T) {
	pm := NewManager(newCoordinator())

	err := pm.RegisterV2(makeSimpleDescriptor("X", []string{"missing-dep"}))
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
	if !strings.Contains(err.Error(), "missing dependency") {
		t.Errorf("expected 'missing dependency' error, got: %v", err)
	}
}

// newCoordinator 创建一个空的 engine 用于测试
func newCoordinator() *engine.Engine {
	return engine.NewEngine()
}
