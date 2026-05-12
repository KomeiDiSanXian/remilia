package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// HostFunc 是宿主函数的签名：接收 JSON 参数，返回 JSON 结果或错误。
type HostFunc func(args json.RawMessage) (json.RawMessage, error)

// HostFuncRegistry 管理 WASM 插件可调用的宿主函数。
type HostFuncRegistry struct {
	mu    sync.RWMutex
	funcs map[string]HostFunc
}

// NewHostFuncRegistry 创建一个空的宿主函数注册表。
func NewHostFuncRegistry() *HostFuncRegistry {
	return &HostFuncRegistry{
		funcs: make(map[string]HostFunc),
	}
}

// Register 注册一个宿主函数。name 应与 WASM 导入名一致。
func (r *HostFuncRegistry) Register(name string, fn HostFunc) {
	if name == "" || fn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.funcs[name] = fn
}

// BuildModule 将已注册的宿主函数构建为 wazero Host Module。
func (r *HostFuncRegistry) BuildModule(ctx context.Context, rt wazero.Runtime) (api.Module, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	builder := rt.NewHostModuleBuilder(HostModuleName)
	for name := range r.funcs {
		funcName := name
		builder.NewFunctionBuilder().
			WithFunc(func(ctx context.Context, module api.Module, ptr uint32, length uint32) uint64 {
				return r.callHostFunc(module, funcName, ptr, length)
			}).
			Export(HostModuleName + "_" + funcName)
	}
	return builder.Instantiate(ctx)
}

// callHostFunc 从 WASM 线性内存读取参数，调用宿主函数，写回结果。
func (r *HostFuncRegistry) callHostFunc(mod api.Module, name string, ptr uint32, length uint32) uint64 {
	r.mu.RLock()
	fn, ok := r.funcs[name]
	r.mu.RUnlock()
	if !ok {
		return 0
	}

	mem := mod.Memory()
	if mem == nil {
		return 0
	}

	var args json.RawMessage
	if length > 0 {
		buf, ok := mem.Read(ptr, length)
		if !ok {
			return 0
		}
		args = make(json.RawMessage, len(buf))
		copy(args, buf)
	}

	result, err := fn(args)
	if err != nil {
		return 0
	}

	if len(result) == 0 {
		return 0
	}

	mallocFn := mod.ExportedFunction(ExportMalloc)
	if mallocFn == nil {
		return 0
	}

	mallocResults, mallocErr := mallocFn.Call(bgCtx, uint64(len(result)))
	if mallocErr != nil || len(mallocResults) == 0 {
		return 0
	}
	allocPtr := uint32(mallocResults[0])

	if ok := mem.Write(allocPtr, result); !ok {
		return 0
	}
	return EncodeResult(allocPtr, uint32(len(result)))
}

// bgCtx 是 wazero 回调中使用的 context.Background。
var bgCtx = context.Background()

// RegisterDefaultHostFuncs 注册默认的宿主函数集。
func RegisterDefaultHostFuncs(r *HostFuncRegistry) {
	r.Register("log", func(args json.RawMessage) (json.RawMessage, error) {
		var req struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, err
		}
		fmt.Printf("[wasm/%s] %s\n", req.Level, req.Message)
		return json.RawMessage("null"), nil
	})

	r.Register("get_config", func(args json.RawMessage) (json.RawMessage, error) {
		var req struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return json.RawMessage("null"), nil
		}
		_ = req.Key
		return json.RawMessage("null"), nil
	})
}
