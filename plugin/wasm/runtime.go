package wasm

import (
	"context"
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
	name       string
	compiled   wazero.CompiledModule
	instance   api.Module
	createdAt  time.Time
	callCount  atomic.Int64
	abiVersion int32
	metadata   []byte
	sandbox    *Sandbox

	runtime *Runtime
}

func (m *Module) ABICompatible() bool {
	if m.abiVersion < 0 {
		return true
	}
	return m.abiVersion == CurrentABIVersion
}

func (m *Module) Metadata(key string) string {
	if len(m.metadata) == 0 {
		return ""
	}
	return string(NewTLVReader(m.metadata).Read(key))
}

func (m *Module) MetadataTLV() []byte { return m.metadata }

func (m *Module) callTimeout() time.Duration {
	if m.sandbox != nil && m.sandbox.CallTimeout > 0 {
		return m.sandbox.CallTimeout
	}
	return DefaultCallTimeout
}

func (m *Module) initTimeout() time.Duration {
	if m.sandbox != nil && m.sandbox.InitTimeout > 0 {
		return m.sandbox.InitTimeout
	}
	return DefaultCallInitTimeout
}

func (m *Module) responseSizeMax() uint32 {
	if m.sandbox != nil && m.sandbox.ResponseSizeMax > 0 {
		return m.sandbox.ResponseSizeMax
	}
	return DefaultResponseSizeMax
}

func (m *Module) wasmSizeMax() uint32 {
	if m.sandbox != nil && m.sandbox.WasmSizeMax > 0 {
		return m.sandbox.WasmSizeMax
	}
	return DefaultWasmSizeMax
}

func (m *Module) importsMax() int {
	if m.sandbox != nil && m.sandbox.ImportsMax > 0 {
		return m.sandbox.ImportsMax
	}
	return DefaultImportsMax
}

// ── CallInit 带超时 ──────────────────────────────────────────────────────────

func (m *Module) CallInit(ctx context.Context) error {
	initFn := m.instance.ExportedFunction(ExportInit)
	if initFn == nil {
		return fmt.Errorf("wasm: module %q missing export %q", m.name, ExportInit)
	}
	callCtx, cancel := context.WithTimeout(ctx, m.initTimeout())
	defer cancel()
	results, err := initFn.Call(callCtx)
	if err != nil {
		return fmt.Errorf("wasm: module %q init call failed: %w", m.name, err)
	}
	if len(results) > 0 && results[0] != 0 {
		return fmt.Errorf("wasm: module %q init returned error code %d", m.name, results[0])
	}
	return nil
}

// ── CallHandle 带超时 + 限流 + 响应大小限制 ─────────────────────────────────

func (m *Module) CallHandle(ctx context.Context, eventTLV []byte) ([]byte, error) {
	if m.sandbox != nil && !m.sandbox.AllowCall() {
		return nil, fmt.Errorf("wasm: %q rate limited", m.name)
	}

	m.callCount.Add(1)

	handleFn := m.instance.ExportedFunction(ExportHandle)
	if handleFn == nil {
		return nil, fmt.Errorf("wasm: module %q missing export %q", m.name, ExportHandle)
	}

	ptr, err := m.allocOrCommArea(ctx, uint32(len(eventTLV)))
	if err != nil {
		return nil, err
	}

	mem := m.instance.Memory()
	if mem == nil {
		return nil, fmt.Errorf("wasm: %q has no memory", m.name)
	}
	if ok := mem.Write(ptr, eventTLV); !ok {
		return nil, fmt.Errorf("wasm: failed to write event data at ptr %d", ptr)
	}

	handleCtx, cancel := context.WithTimeout(ctx, m.callTimeout())
	defer cancel()
	results, err := handleFn.Call(handleCtx, uint64(ptr), uint64(len(eventTLV)))
	if err != nil {
		return nil, fmt.Errorf("wasm: handle call failed: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}

	var respPtr, respLen uint32
	if len(results) >= 2 {
		respPtr = uint32(results[0])
		respLen = uint32(results[1])
	} else {
		respPtr, respLen = DecodeResult(results[0])
	}
	if respPtr == 0 || respLen == 0 {
		return nil, nil
	}

	maxResp := m.responseSizeMax()
	if respLen > maxResp {
		return nil, fmt.Errorf("wasm: %q response %d exceeds max %d", m.name, respLen, maxResp)
	}

	buf, ok := mem.Read(respPtr, respLen)
	if !ok {
		return nil, fmt.Errorf("wasm: failed to read response at ptr %d len %d", respPtr, respLen)
	}
	resp := make([]byte, respLen)
	copy(resp, buf)
	return resp, nil
}

// allocOrCommArea 内存分配（带大小检查）
func (m *Module) allocOrCommArea(ctx context.Context, size uint32) (uint32, error) {
	if size > CommAreaSize {
		return 0, fmt.Errorf("wasm: allocation %d exceeds comm area %d", size, CommAreaSize)
	}
	if mallocFn := m.instance.ExportedFunction(ExportMalloc); mallocFn != nil {
		results, err := mallocFn.Call(ctx, uint64(size))
		if err == nil && len(results) > 0 && results[0] != 0 {
			return uint32(results[0]), nil
		}
	}
	return CommAreaOffset, nil
}

func (m *Module) Close(ctx context.Context) error {
	return m.instance.Close(ctx)
}

func (m *Module) Name() string          { return m.name }
func (m *Module) CallCount() int64      { return m.callCount.Load() }
func (m *Module) Uptime() time.Duration { return time.Since(m.createdAt) }

// ── Runtime ──────────────────────────────────────────────────────────────────

type Runtime struct {
	wazeroRuntime wazero.Runtime
	hostRegistry  *HostFuncRegistry
}

func NewRuntime(ctx context.Context, hostRegistry *HostFuncRegistry) (*Runtime, error) {
	if hostRegistry == nil {
		hostRegistry = NewHostFuncRegistry()
		RegisterDefaultHostFuncs(hostRegistry)
	}

	rtConfig := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true)
	wazeroRuntime := wazero.NewRuntimeWithConfig(ctx, rtConfig)

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, wazeroRuntime); err != nil {
		wazeroRuntime.Close(ctx)
		return nil, fmt.Errorf("wasm: wasi instantiation failed: %w", err)
	}

	if _, err := hostRegistry.BuildModule(ctx, wazeroRuntime); err != nil {
		wazeroRuntime.Close(ctx)
		return nil, fmt.Errorf("wasm: host module instantiation failed: %w", err)
	}

	return &Runtime{
		wazeroRuntime: wazeroRuntime,
		hostRegistry:  hostRegistry,
	}, nil
}

func (r *Runtime) LoadModule(ctx context.Context, name, path string, sandbox *Sandbox) (*Module, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wasm: failed to read %q: %w", path, err)
	}
	return r.InstantiateModule(ctx, name, wasmBytes, sandbox)
}

// InstantiateModule 实例化 WASM 模块（带安全验证和限制）。
func (r *Runtime) InstantiateModule(ctx context.Context, name string, wasmBytes []byte, sandbox *Sandbox) (*Module, error) {
	// 从 sandbox 获取安全阈值，无 sandbox 时使用默认值
	wasmMax := uint32(DefaultWasmSizeMax)
	impMax := DefaultImportsMax
	initTO := DefaultCallInitTimeout
	if sandbox != nil {
		if sandbox.WasmSizeMax > 0 {
			wasmMax = sandbox.WasmSizeMax
		}
		if sandbox.ImportsMax > 0 {
			impMax = sandbox.ImportsMax
		}
		if sandbox.InitTimeout > 0 {
			initTO = sandbox.InitTimeout
		}
	}

	if uint32(len(wasmBytes)) > wasmMax {
		return nil, fmt.Errorf("wasm: %q size %d exceeds max %d", name, len(wasmBytes), wasmMax)
	}

	compiled, err := r.wazeroRuntime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("wasm: compile %q failed: %w", name, err)
	}

	imports := compiled.ImportedFunctions()
	if len(imports) > impMax {
		compiled.Close(ctx)
		return nil, fmt.Errorf("wasm: %q imports %d exceeds max %d", name, len(imports), impMax)
	}

	config := wazero.NewModuleConfig().
		WithName(name).
		WithStartFunctions()

	instance, err := r.wazeroRuntime.InstantiateModule(ctx, compiled, config)
	if err != nil {
		compiled.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiate %q failed: %w", name, err)
	}

	// _start 带超时
	if startFn := instance.ExportedFunction("_start"); startFn != nil {
		startCtx, startCancel := context.WithTimeout(ctx, initTO)
		defer startCancel()
		if _, err := startFn.Call(startCtx); err != nil {
			instance.Close(ctx)
			compiled.Close(ctx)
			return nil, fmt.Errorf("wasm: _start %q failed: %w", name, err)
		}
	}

	// 验证必需导出
	for _, exp := range []string{ExportInit, ExportHandle} {
		if instance.ExportedFunction(exp) == nil {
			instance.Close(ctx)
			compiled.Close(ctx)
			return nil, fmt.Errorf("wasm: module %q missing required export %q", name, exp)
		}
	}

	// 读取 ABI 版本
	var abiVersion int32 = -1
	if abiFn := instance.ExportedFunction(ExportABIVersion); abiFn != nil {
		if results, err := abiFn.Call(ctx); err == nil && len(results) > 0 {
			abiVersion = int32(results[0])
		}
	}

	// 读取元数据（带大小限制）
	var metadata []byte
	if metaFn := instance.ExportedFunction(ExportMetadata); metaFn != nil {
		if results, err := metaFn.Call(ctx); err == nil && len(results) >= 2 {
			metaPtr := uint32(results[0])
			metaLen := uint32(results[1])
			if metaPtr != 0 && metaLen > 0 && metaLen <= 4096 {
				if buf, ok := instance.Memory().Read(metaPtr, metaLen); ok {
					metadata = make([]byte, metaLen)
					copy(metadata, buf)
				}
			}
		}
	}

	if abiVersion >= 0 && abiVersion != CurrentABIVersion {
		fmt.Printf("[wasm] %q ABI v%d != host v%d\n", name, abiVersion, CurrentABIVersion)
	}

	return &Module{
		name:       name,
		compiled:   compiled,
		instance:   instance,
		createdAt:  time.Now(),
		abiVersion: abiVersion,
		metadata:   metadata,
		sandbox:    sandbox,
		runtime:    r,
	}, nil
}

func (r *Runtime) Close(ctx context.Context) error {
	return r.wazeroRuntime.Close(ctx)
}
