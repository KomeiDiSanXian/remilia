package wasm_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

const tinygoWasmPath = "testdata/tinygoplugin.wasm"

// requireTinyGoWasm 检查 TinyGo 编译的 WASM 文件是否存在，不存在则跳过。
func requireTinyGoWasm(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(tinygoWasmPath); os.IsNotExist(err) {
		t.Skipf("跳过 TinyGo 测试：%s 不存在", tinygoWasmPath)
	}
}

func TestTinyGo_Runtime_Instantiate(t *testing.T) {
	requireTinyGoWasm(t)

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	wasmBytes, err := os.ReadFile(tinygoWasmPath)
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, "tinygo-test", wasmBytes, nil)
	require.NoError(t, err, "TinyGo WASM 模块应能正常实例化")
	defer mod.Close(ctx)

	assert.Equal(t, "tinygo-test", mod.Name())
}

func TestTinyGo_Module_CallInit(t *testing.T) {
	requireTinyGoWasm(t)

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	wasmBytes, err := os.ReadFile(tinygoWasmPath)
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, "tinygo-test", wasmBytes, nil)
	require.NoError(t, err)
	defer mod.Close(ctx)

	err = mod.CallInit(ctx)
	require.NoError(t, err, "TinyGo plugin_init 应返回 0")
}

func TestTinyGo_Module_CallHandle_Ping(t *testing.T) {
	requireTinyGoWasm(t)

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	wasmBytes, err := os.ReadFile(tinygoWasmPath)
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, "tinygo-test", wasmBytes, nil)
	require.NoError(t, err)
	defer mod.Close(ctx)

	// Call init first
	err = mod.CallInit(ctx)
	require.NoError(t, err)

	// Send /wasmping (TLV encoded)
	eventTLV := wasm.NewTLVBuilder().WriteString("c", "/wasmping").Bytes()
	respTLV, err := mod.CallHandle(ctx, eventTLV)
	require.NoError(t, err)
	require.NotNil(t, respTLV, "TinyGo 模块应回复 /wasmping")

	reply := wasm.NewTLVReader(respTLV).ReadString("r")
	assert.Equal(t, "pong from wasm", reply)
}

func TestTinyGo_Module_CallHandle_Hello(t *testing.T) {
	requireTinyGoWasm(t)

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	wasmBytes, err := os.ReadFile(tinygoWasmPath)
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, "tinygo-test", wasmBytes, nil)
	require.NoError(t, err)
	defer mod.Close(ctx)

	err = mod.CallInit(ctx)
	require.NoError(t, err)

	// Send /wasmhello (TLV encoded)
	eventTLV := wasm.NewTLVBuilder().WriteString("c", "/wasmhello").Bytes()
	respTLV, err := mod.CallHandle(ctx, eventTLV)
	require.NoError(t, err)
	require.NotNil(t, respTLV)

	reply := wasm.NewTLVReader(respTLV).ReadString("r")
	assert.Contains(t, reply, "WASM", "应回复包含 WASM 的消息")
}

func TestTinyGo_Module_MultipleCalls(t *testing.T) {
	requireTinyGoWasm(t)

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	wasmBytes, err := os.ReadFile(tinygoWasmPath)
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, "tinygo-test", wasmBytes, nil)
	require.NoError(t, err)
	defer mod.Close(ctx)

	err = mod.CallInit(ctx)
	require.NoError(t, err)

	// Make multiple calls
	eventTLV := wasm.NewTLVBuilder().WriteString("c", "/wasmping").Bytes()
	for range 5 {
		respTLV, err := mod.CallHandle(ctx, eventTLV)
		require.NoError(t, err)
		require.NotNil(t, respTLV)
	}

	assert.Equal(t, int64(5), mod.CallCount(), "应记录 5 次调用")
}

func TestTinyGo_Manager_FullLifecycle(t *testing.T) {
	requireTinyGoWasm(t)

	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	mgr := wasm.NewManager(eng, nil)
	ctx := context.Background()

	wasmBytes, err := os.ReadFile(tinygoWasmPath)
	require.NoError(t, err)

	desc := &wasm.Descriptor{
		Name:    "tinygo-demo",
		Version: "1.0.0",
		Commands: []wasm.CommandDef{
			{EventType: "", Command: "/wasmhello"},
			{EventType: "", Command: "/wasmping"},
		},
	}

	inst, err := mgr.Instantiate(ctx, desc, wasmBytes)
	require.NoError(t, err)
	require.NotNil(t, inst)
	assert.Equal(t, "tinygo-demo", inst.Desc.Name)

	assert.Contains(t, mgr.List(), "tinygo-demo")

	err = mgr.Unregister(ctx, "tinygo-demo")
	require.NoError(t, err)
	assert.NotContains(t, mgr.List(), "tinygo-demo")
}

func TestTinyGo_Manager_RegisterFromFile(t *testing.T) {
	requireTinyGoWasm(t)

	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	mgr := wasm.NewManager(eng, nil)
	ctx := context.Background()

	desc := &wasm.Descriptor{
		Name:    "tinygo-demo",
		Version: "1.0.0",
		Path:    tinygoWasmPath,
		Commands: []wasm.CommandDef{
			{EventType: "", Command: "/wasmping"},
		},
	}

	inst, err := mgr.Register(ctx, desc)
	require.NoError(t, err, "从文件加载 TinyGo WASM 插件")
	assert.Equal(t, "tinygo-demo", inst.Desc.Name)

	err = mgr.Unregister(ctx, "tinygo-demo")
	require.NoError(t, err)
}

// ── ABI 版本和自描述测试 ────────────────────────────────────────────────────

func TestTinyGo_ABIVersion(t *testing.T) {
	requireTinyGoWasm(t)

	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	wasmBytes, err := os.ReadFile(tinygoWasmPath)
	require.NoError(t, err)

	mod, err := rt.InstantiateModule(ctx, "tinygo-test", wasmBytes, nil)
	require.NoError(t, err)
	defer mod.Close(ctx)

	assert.True(t, mod.ABICompatible(), "TinyGo 插件应声明兼容 ABI v2")
}

func TestHostFuncRegistry_ListFunctions(t *testing.T) {
	reg := wasm.NewHostFuncRegistry()
	wasm.RegisterDefaultHostFuncs(reg)

	names := reg.ListFunctionNames()
	assert.Contains(t, names, "log", "默认注册表应包含 log")
	assert.Contains(t, names, "get_config", "默认注册表应包含 get_config")
}

func TestHostFuncRegistry_DefaultIncludesSelfDesc(t *testing.T) {
	reg := wasm.NewHostFuncRegistry()
	wasm.RegisterDefaultHostFuncs(reg)

	names := reg.ListFunctionNames()
	assert.Contains(t, names, "__host_abi_version", "默认注册表应包含 __host_abi_version")
	assert.Contains(t, names, "__host_functions", "默认注册表应包含 __host_functions")
}
