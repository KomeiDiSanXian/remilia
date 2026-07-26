package wasm

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// HostFunc 是宿主函数的签名：接收 TLV 参数，返回 TLV 结果或错误。
type HostFunc func(args []byte) ([]byte, error)

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
				return r.callHostFunc(ctx, module, funcName, ptr, length)
			}).
			Export(funcName)
	}
	return builder.Instantiate(ctx)
}

// callHostFunc 从 WASM 线性内存读取参数，调用宿主函数，写回结果。
// ctx 必须是 wazero 传入宿主函数的调用 context——回调 guest 的 malloc 时
// 复用它，才能继承 WithCloseOnContextDone 的超时/取消控制。
func (r *HostFuncRegistry) callHostFunc(ctx context.Context, mod api.Module, name string, ptr uint32, length uint32) uint64 {
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

	var args []byte
	if length > 0 {
		buf, ok := mem.Read(ptr, length)
		if !ok {
			return 0
		}
		args = make([]byte, len(buf))
		copy(args, buf)
	}

	result, err := fn(args)
	if err != nil || len(result) == 0 {
		return 0
	}

	mallocFn := mod.ExportedFunction(ExportMalloc)
	if mallocFn == nil {
		return 0
	}

	mallocResults, mallocErr := mallocFn.Call(ctx, uint64(len(result)))
	if mallocErr != nil || len(mallocResults) == 0 {
		return 0
	}
	allocPtr := uint32(mallocResults[0])

	if ok := mem.Write(allocPtr, result); !ok {
		return 0
	}
	return EncodeResult(allocPtr, uint32(len(result)))
}

// ListFunctionNames 返回已注册的所有宿主函数名。
func (r *HostFuncRegistry) ListFunctionNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.funcs))
	for n := range r.funcs {
		names = append(names, n)
	}
	return names
}

// RegisterDefaultHostFuncs 注册默认的宿主函数集，包括自描述函数。
// get_config 默认返回空值；Runtime 创建后可通过 HostFuncRegistry.Register
// 替换为带配置查找的实现。
func RegisterDefaultHostFuncs(r *HostFuncRegistry) {
	r.Register("log", func(args []byte) ([]byte, error) {
		level := NewTLVReader(args).ReadString("level")
		msg := NewTLVReader(args).ReadString("message")
		if msg == "" {
			// 兼容单参数调用：直接取第一个 TLV 值作为消息
			msg = NewTLVReader(args).ReadString("c")
			if msg == "" {
				msg = string(args)
			}
		}
		if level == "" {
			level = "info"
		}
		fmt.Printf("[wasm/%s] %s\n", level, msg)
		return []byte("null"), nil
	})

	r.Register("get_config", func(args []byte) ([]byte, error) {
		// 此函数在 Runtime 创建后会被替换为带配置查找的实现。
		// 若未被替换，则返回空值以保持向前兼容。
		return NewTLVBuilder().WriteString("v", "").Bytes(), nil
	})

	// 自描述宿主函数：返回宿主 ABI 版本
	r.Register(HostFuncABIVersion, func(args []byte) ([]byte, error) {
		return NewTLVBuilder().WriteString("v", fmt.Sprintf("%d", CurrentABIVersion)).Bytes(), nil
	})

	// 自描述宿主函数：返回可用函数列表（TLV 多键）
	r.Register(HostFuncListFunctions, func(args []byte) ([]byte, error) {
		names := r.ListFunctionNames()
		b := NewTLVBuilder()
		for _, n := range names {
			if n == HostFuncListFunctions || n == HostFuncABIVersion {
				continue // 不列出自描述函数自身以避免递归
			}
			b.WriteString("f", n)
		}
		return b.Bytes(), nil
	})
}
