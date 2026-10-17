package wasm_test

// wasm_lifecycle_test.go — 编译产物回收、默认宿主函数集与 Bridge 空值防御回归测试
//
// 回归背景：
//   - Module.Close 只关闭实例，不关闭编译产物（wazero.CompiledModule）：
//     每次卸载/重载都会留一份编译产物，直到整个运行时才回收；
//   - abi.go 声明了 register_command/send_message/storage_* /http_request，
//     但框架默认只提供 log/get_config/两个自描述函数，文档却宣称它们存在
//     （插件导入未提供的函数会实例化失败）；
//   - Bridge 的 handler 直接调用 ctx.GetPlatformEvent().ID()，无平台事件的
//     Context（合成事件）会让 handler panic。

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

func TestModule_CloseReleasesCompiledModule(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	assert.Equal(t, 0, rt.OpenModules())

	mod, err := rt.InstantiateModule(ctx, "close-test", testWasmModule, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, rt.OpenModules(), "实例化后应计入未释放模块")
	assert.False(t, mod.Closed())

	require.NoError(t, mod.Close(ctx))
	assert.True(t, mod.Closed())
	assert.Equal(t, 0, rt.OpenModules(), "Close 必须同时释放编译产物")

	// 幂等：重复 Close 不改变计数、不报新错误
	require.NoError(t, mod.Close(ctx))
	assert.Equal(t, 0, rt.OpenModules())
}

func TestManager_UnregisterReleasesCompiledModule(t *testing.T) {
	ctx := context.Background()
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Shutdown(shutdownCtx)
	})

	mgr := wasm.NewManager(eng, nil)
	inst, err := mgr.Instantiate(ctx, &wasm.Descriptor{Name: "reload-leak"}, testWasmModule)
	require.NoError(t, err)
	require.NotNil(t, inst)

	rt := mgr.GetRuntime()
	require.NotNil(t, rt)
	assert.Equal(t, 1, rt.OpenModules())

	require.NoError(t, mgr.Unregister(ctx, "reload-leak"))
	assert.Equal(t, 0, rt.OpenModules(), "卸载必须释放编译产物，否则每次重载累积一份")

	// 重新注册同名插件（模拟重载）后依然能正常回收
	_, err = mgr.Instantiate(ctx, &wasm.Descriptor{Name: "reload-leak"}, testWasmModule)
	require.NoError(t, err)
	assert.Equal(t, 1, rt.OpenModules())
	require.NoError(t, mgr.Unregister(ctx, "reload-leak"))
	assert.Equal(t, 0, rt.OpenModules())
}

func TestManager_DuplicateRegisterReleasesLosingInstance(t *testing.T) {
	ctx := context.Background()
	mgr := wasm.NewManager(nil, nil)

	_, err := mgr.Instantiate(ctx, &wasm.Descriptor{Name: "dup"}, testWasmModule)
	require.NoError(t, err)

	rt := mgr.GetRuntime()
	before := rt.OpenModules()

	_, err = mgr.Instantiate(ctx, &wasm.Descriptor{Name: "dup"}, testWasmModule)
	require.Error(t, err, "同名注册必须被拒绝")
	assert.Equal(t, before, rt.OpenModules(), "失败者必须释放自己准备好的模块")
}

func TestRegisterDefaultHostFuncs_MatchesDocumentedSet(t *testing.T) {
	reg := wasm.NewHostFuncRegistry()
	wasm.RegisterDefaultHostFuncs(reg)

	assert.ElementsMatch(t,
		[]string{"log", "get_config", wasm.HostFuncABIVersion, wasm.HostFuncListFunctions},
		reg.ListFunctionNames(),
		"默认宿主函数集必须与 abi.go/文档一致（新增默认函数需同步文档）")

	// 约定名不应出现在默认集合里：它们需要宿主应用注册
	for _, name := range []string{
		wasm.HostFuncRegisterCmd,
		wasm.HostFuncSendMessage,
		wasm.HostFuncStorageGet,
		wasm.HostFuncStorageSet,
		wasm.HostFuncHTTPRequest,
	} {
		assert.NotContains(t, reg.ListFunctionNames(), name,
			"%s 是约定名，框架默认不提供", name)
	}

	// Runtime 注入的 get_config 覆盖默认实现，集合保持不变
	rt, err := wasm.NewRuntime(context.Background(), reg)
	require.NoError(t, err)
	defer rt.Close(context.Background())
	assert.ElementsMatch(t,
		[]string{"log", "get_config", wasm.HostFuncABIVersion, wasm.HostFuncListFunctions},
		reg.ListFunctionNames())
}

func TestBridge_HandlerWithoutPlatformEventDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	eng := engine.NewEngine(engine.WithNoBackgroundWorkers())
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.Shutdown(shutdownCtx)
	})

	mgr := wasm.NewManager(eng, nil)

	inst, err := mgr.Instantiate(ctx, &wasm.Descriptor{Name: "nil-event"}, testWasmModule)
	require.NoError(t, err)

	m, err := inst.Bridge.RegisterCommand(wasm.RegistrationRequest{EventType: "", Command: ""})
	require.NoError(t, err)
	require.NotNil(t, m.Handler)

	// 无平台事件的 Context：Bridge 必须容错（此前 GetPlatformEvent().ID() 会 panic）。
	// 直接调用 handler：经引擎派发时 panic 会被引擎恢复，测不出根因。
	bare := corectx.NewContextFromEvent(nil, nil)
	require.NotPanics(t, func() {
		_ = m.Handler(bare)
	})
}
