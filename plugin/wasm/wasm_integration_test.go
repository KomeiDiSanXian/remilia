package wasm_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

var testWasmModule = newTestWasmModule()

// tlvEvent 使用 TLV 编码测试事件。
func tlvEvent(content string) []byte {
	return wasm.NewTLVBuilder().WriteString("c", content).Bytes()
}

// ── Runtime + Module 集成测试 ─────────────────────────────────────────────

func TestRuntime_InstantiateModule(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, "test-plugin", testWasmModule, nil)
	require.NoError(t, err, "WASM 模块应能正常实例化")
	defer mod.Close(ctx)

	assert.Equal(t, "test-plugin", mod.Name())
	assert.Equal(t, int64(0), mod.CallCount())
	assert.WithinDuration(t, time.Now(), time.Now().Add(-mod.Uptime()), 5*time.Second)
}

func TestModule_CallInit(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod := loadTestModule(t, ctx, rt)
	defer mod.Close(ctx)

	err = mod.CallInit(ctx)
	require.NoError(t, err, "plugin_init 应返回 0")
}

func TestModule_CallHandle_NoReply(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod := loadTestModule(t, ctx, rt)
	defer mod.Close(ctx)
	require.NoError(t, mod.CallInit(ctx))

	resp, err := mod.CallHandle(ctx, tlvEvent("/ping"))
	require.NoError(t, err)
	assert.Nil(t, resp, "基本测试模块应返回 nil（无回复）")
}

func TestModule_CallCount(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod := loadTestModule(t, ctx, rt)
	defer mod.Close(ctx)
	require.NoError(t, mod.CallInit(ctx))

	for range 5 {
		_, err := mod.CallHandle(ctx, tlvEvent("test"))
		require.NoError(t, err)
	}

	assert.Equal(t, int64(5), mod.CallCount(), "应记录 5 次调用")
}

// ── HostFuncRegistry 集成测试 ──────────────────────────────────────────────

func TestHostFuncRegistry_CustomFunc(t *testing.T) {
	reg := wasm.NewHostFuncRegistry()
	wasm.RegisterDefaultHostFuncs(reg)
	var captured string
	reg.Register("custom_test", func(args []byte) ([]byte, error) {
		captured = string(args)
		return []byte("ok"), nil
	})

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, reg)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod := loadTestModule(t, ctx, rt)
	defer mod.Close(ctx)
	assert.NotNil(t, mod)
	_ = captured
}

// ── Manager 集成测试 ───────────────────────────────────────────────────────

func TestManager_FullLifecycle(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	mgr := wasm.NewManager(eng, nil)
	ctx := context.Background()

	desc := &wasm.Descriptor{
		Name:    "demo",
		Version: "1.0.0",
		Commands: []wasm.CommandDef{
			{EventType: "", Command: "/wasmhello"},
			{EventType: "", Command: "/wasmping"},
		},
	}

	inst, err := mgr.Instantiate(ctx, desc, testWasmModule)
	require.NoError(t, err)
	require.NotNil(t, inst)
	assert.Equal(t, "demo", inst.Desc.Name)

	assert.Contains(t, mgr.List(), "demo")

	got := mgr.Get("demo")
	require.NotNil(t, got)
	assert.Nil(t, mgr.Get("nonexistent"))

	desc2 := &wasm.Descriptor{Name: "demo"}
	_, err = mgr.Instantiate(ctx, desc2, testWasmModule)
	assert.Error(t, err, "重复名称应被拒绝")

	err = mgr.Unregister(ctx, "demo")
	require.NoError(t, err)
	assert.NotContains(t, mgr.List(), "demo")
}

func TestManager_Close(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	mgr := wasm.NewManager(eng, nil)
	ctx := context.Background()

	_, err := mgr.Instantiate(ctx,
		&wasm.Descriptor{Name: "demo", Commands: []wasm.CommandDef{{Command: "/test"}}},
		testWasmModule)
	require.NoError(t, err)

	err = mgr.Close(ctx)
	require.NoError(t, err)
	assert.Empty(t, mgr.List())
}

// ── Bridge + Descriptor 集成测试 ──────────────────────────────────────────

func TestDescriptor_WithCommands(t *testing.T) {
	d := &wasm.Descriptor{
		Name: "test",
		Path: "/x.wasm",
		Commands: []wasm.CommandDef{
			{EventType: "", Command: "/hello"},
			{EventType: "", Command: "/ping"},
		},
	}
	assert.NoError(t, d.Validate())
	assert.Len(t, d.Commands, 2)
}

func TestBridge_RegisterCommand(t *testing.T) {
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, "test", testWasmModule, nil)
	require.NoError(t, err)
	defer mod.Close(ctx)

	bridge := wasm.NewBridge(mod, eng)
	matcher, err := bridge.RegisterCommand(wasm.RegistrationRequest{
		EventType: "",
		Command:   "/wasmhello",
	})
	require.NoError(t, err)
	require.NotNil(t, matcher)

	bridge.Cleanup()
}

// ── HostFuncRegistry BuildModule 验证 ─────────────────────────────────────

func TestHostFuncRegistry_BuildModule(t *testing.T) {
	reg := wasm.NewHostFuncRegistry()
	wasm.RegisterDefaultHostFuncs(reg)
	called := false
	reg.Register("test_fn", func(args []byte) ([]byte, error) {
		called = true
		return []byte("ok"), nil
	})

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, reg)
	require.NoError(t, err)
	defer rt.Close(ctx)
	assert.NotNil(t, rt)
	_ = called
}

// ── 辅助函数 ──────────────────────────────────────────────────────────────

func loadTestModule(t *testing.T, ctx context.Context, rt *wasm.Runtime) *wasm.Module {
	t.Helper()
	mod, err := rt.InstantiateModule(ctx, "test-plugin", testWasmModule, nil)
	require.NoError(t, err)
	return mod
}
