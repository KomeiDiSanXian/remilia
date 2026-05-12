package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Module 表示一个已加载的 WASM 模块实例。
type Module struct {
	name      string
	compiled  wazero.CompiledModule
	instance  api.Module
	createdAt time.Time
	callCount atomic.Int64

	runtime *Runtime
}

// CallInit 调用 WASM 模块的 plugin_init 导出函数。
// 返回 nil 表示初始化成功，非 nil 表示初始化失败。
func (m *Module) CallInit(ctx context.Context) error {
	initFn := m.instance.ExportedFunction(ExportInit)
	if initFn == nil {
		return fmt.Errorf("wasm: module %q missing export %q", m.name, ExportInit)
	}
	results, err := initFn.Call(ctx)
	if err != nil {
		return fmt.Errorf("wasm: module %q init call failed: %w", m.name, err)
	}
	if len(results) > 0 && results[0] != 0 {
		return fmt.Errorf("wasm: module %q init returned error code %d", m.name, results[0])
	}
	return nil
}

// CallHandle 调用 WASM 模块的 plugin_handle 导出函数。
// eventJSON 是序列化的事件数据，返回插件回复的 JSON。
func (m *Module) CallHandle(ctx context.Context, eventJSON []byte) (json.RawMessage, error) {
	m.callCount.Add(1)

	handleFn := m.instance.ExportedFunction(ExportHandle)
	if handleFn == nil {
		return nil, fmt.Errorf("wasm: module %q missing export %q", m.name, ExportHandle)
	}

	// 将事件数据写入 WASM 线性内存
	mallocFn := m.instance.ExportedFunction(ExportMalloc)
	if mallocFn == nil {
		return nil, fmt.Errorf("wasm: module %q missing export %q", m.name, ExportMalloc)
	}

	mallocResult, err := mallocFn.Call(ctx, uint64(len(eventJSON)))
	if err != nil {
		return nil, fmt.Errorf("wasm: malloc failed: %w", err)
	}
	if len(mallocResult) == 0 {
		return nil, fmt.Errorf("wasm: malloc returned empty")
	}
	ptr := uint32(mallocResult[0])

	if ok := m.instance.Memory().Write(ptr, eventJSON); !ok {
		return nil, fmt.Errorf("wasm: failed to write event data at ptr %d", ptr)
	}

	// 调用 handle
	results, err := handleFn.Call(ctx, uint64(ptr), uint64(len(eventJSON)))
	if err != nil {
		return nil, fmt.Errorf("wasm: handle call failed: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}

	respPtr, respLen := DecodeResult(results[0])
	if respPtr == 0 || respLen == 0 {
		return nil, nil
	}

	buf, ok := m.instance.Memory().Read(respPtr, respLen)
	if !ok {
		return nil, fmt.Errorf("wasm: failed to read response at ptr %d len %d", respPtr, respLen)
	}
	resp := make(json.RawMessage, respLen)
	copy(resp, buf)
	return resp, nil
}

// Close 关闭 WASM 模块实例。
func (m *Module) Close(ctx context.Context) error {
	return m.instance.Close(ctx)
}

// Name 返回模块名称。
func (m *Module) Name() string { return m.name }

// CallCount 返回模块被调用的次数。
func (m *Module) CallCount() int64 { return m.callCount.Load() }

// Uptime 返回模块已运行的时间。
func (m *Module) Uptime() time.Duration { return time.Since(m.createdAt) }

// Runtime 封装 wazero.Runtime，负责 WASM 模块的加载和管理。
type Runtime struct {
	wazeroRuntime wazero.Runtime
	hostRegistry  *HostFuncRegistry
}

// NewRuntime 创建一个 WASM 运行时，注册默认宿主函数。
// 调用者应在不再使用时调用 [Runtime.Close]。
func NewRuntime(ctx context.Context, hostRegistry *HostFuncRegistry) (*Runtime, error) {
	if hostRegistry == nil {
		hostRegistry = NewHostFuncRegistry()
		RegisterDefaultHostFuncs(hostRegistry)
	}

	rtConfig := wazero.NewRuntimeConfig()
	wazeroRuntime := wazero.NewRuntimeWithConfig(ctx, rtConfig)

	// 注册 WASI（提供基本的 POSIX I/O 支持）
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, wazeroRuntime); err != nil {
		wazeroRuntime.Close(ctx)
		return nil, fmt.Errorf("wasm: wasi instantiation failed: %w", err)
	}

	// 注册宿主函数模块
	if _, err := hostRegistry.BuildModule(ctx, wazeroRuntime); err != nil {
		wazeroRuntime.Close(ctx)
		return nil, fmt.Errorf("wasm: host module instantiation failed: %w", err)
	}

	return &Runtime{
		wazeroRuntime: wazeroRuntime,
		hostRegistry:  hostRegistry,
	}, nil
}

// LoadModule 从文件路径加载一个 .wasm 模块。
// name 是模块的标识名，用于调试和错误信息。
func (r *Runtime) LoadModule(ctx context.Context, name, path string) (*Module, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wasm: failed to read %q: %w", path, err)
	}
	return r.InstantiateModule(ctx, name, wasmBytes)
}

// InstantiateModule 从字节数据实例化一个 WASM 模块。
func (r *Runtime) InstantiateModule(ctx context.Context, name string, wasmBytes []byte) (*Module, error) {
	compiled, err := r.wazeroRuntime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("wasm: compile %q failed: %w", name, err)
	}

	config := wazero.NewModuleConfig().
		WithName(name).
		WithStartFunctions()

	instance, err := r.wazeroRuntime.InstantiateModule(ctx, compiled, config)
	if err != nil {
		compiled.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiate %q failed: %w", name, err)
	}

	// 验证必要导出是否存在
	for _, exp := range []string{ExportInit, ExportHandle, ExportMalloc} {
		if instance.ExportedFunction(exp) == nil {
			instance.Close(ctx)
			compiled.Close(ctx)
			return nil, fmt.Errorf("wasm: module %q missing required export %q", name, exp)
		}
	}

	return &Module{
		name:      name,
		compiled:  compiled,
		instance:  instance,
		createdAt: time.Now(),
		runtime:   r,
	}, nil
}

// Close 关闭 WASM 运行时并释放所有资源。
func (r *Runtime) Close(ctx context.Context) error {
	return r.wazeroRuntime.Close(ctx)
}
