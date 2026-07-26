package wasm

import (
	"fmt"
	"time"
)

// CommandDef 定义 WASM 插件在宿主侧注册的命令。
// 采用声明式注册：在 Descriptor 中声明命令列表，由 Manager 在加载时
// 通过 Bridge 注册到 Engine，无需 WASM 模块在 plugin_init 中自注册。
type CommandDef struct {
	// EventType 事件类型，空字符串表示默认消息事件。
	EventType string
	// Command 命令模式，例如 "/wasmhello"。
	Command string
}

// Descriptor 描述一个 WASM 插件的配置。
// 与 [plugin.Descriptor] 不同，Descriptor 不包含 Setup/Teardown 回调，
// 而是通过加载 .wasm 文件并调用其导出的函数来完成生命周期管理。
type Descriptor struct {
	// Name 是插件的唯一标识。
	Name string
	// Version 是插件的版本号（可选，建议 semver）。
	Version string
	// Path 是 .wasm 文件的路径（用于 wasm.Manager.Register）。
	Path string
	// Config 是传递给插件的配置（JSON 序列化后通过 get_config 宿主函数读取）。
	Config map[string]any
	// Commands 是插件声明的命令列表，由 Manager 在加载时注册到 Engine。
	Commands []CommandDef
	// ResourceLimit 可选资源限制。为 nil 时使用默认值。
	ResourceLimit *ResourceLimit
	// CallTimeout 单次 handle 调用的超时时间（ResourceLimit.CallTimeout 的便捷别名，
	// 两者同时设置时以 ResourceLimit 为准）。零值表示使用默认值（30s）。
	CallTimeout time.Duration
	// Deps 声明此 WASM 插件依赖的 Go 插件列表。仅在使用 ToDescriptor/
	// RegisterWithManager 时生效，确保依赖先于 WASM 模块加载。
	Deps []string
}

// ResourceLimit 定义 WASM 插件的所有资源限制和安全阈值。
// 零值字段使用包级别的默认值。
type ResourceLimit struct {
	// MemoryPages 最大内存页数（每页 64KB）。默认 256（16MB）。
	// 仅在独占 Runtime 路径（ToDescriptor）强制生效；
	// wasm.Manager 的共享 Runtime 无法按插件区分，不做限制。
	MemoryPages uint32
	// MaxCallPerSec 每秒最大调用次数。默认 1000。
	MaxCallPerSec int64
	// CallTimeout handle 单次调用的超时时间。默认 30s。
	CallTimeout time.Duration
	// InitTimeout plugin_init / _start 的超时时间。默认 10s。
	InitTimeout time.Duration
	// ResponseSizeMax 插件单次响应最大字节数。默认 1MB。
	ResponseSizeMax uint32
	// WasmSizeMax 加载的 .wasm 文件最大字节数。默认 50MB。
	WasmSizeMax uint32
	// ImportsMax 插件允许的最大导入函数数。默认 100。
	ImportsMax int
}

// Validation 校验 Descriptor 的必填字段。
func (d *Descriptor) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("wasm: Descriptor.Name is required")
	}
	if d.Path == "" {
		return fmt.Errorf("wasm: Descriptor.Path is required")
	}
	return nil
}

// EffectiveResourceLimit 返回生效的资源限制，零值字段用默认值填充。
func (d *Descriptor) EffectiveResourceLimit() ResourceLimit {
	rl := ResourceLimit{
		MemoryPages:     DefaultMemoryPages,
		MaxCallPerSec:   DefaultMaxCallPerSec,
		CallTimeout:     DefaultCallTimeout,
		InitTimeout:     DefaultCallInitTimeout,
		ResponseSizeMax: DefaultResponseSizeMax,
		WasmSizeMax:     DefaultWasmSizeMax,
		ImportsMax:      DefaultImportsMax,
	}
	// Descriptor.CallTimeout 是 ResourceLimit.CallTimeout 的便捷别名，
	// 后者优先（旧实现完全忽略此字段，设了等于没设）。
	if d.CallTimeout > 0 {
		rl.CallTimeout = d.CallTimeout
	}
	if d.ResourceLimit != nil {
		if d.ResourceLimit.MemoryPages > 0 {
			rl.MemoryPages = d.ResourceLimit.MemoryPages
		}
		if d.ResourceLimit.MaxCallPerSec > 0 {
			rl.MaxCallPerSec = d.ResourceLimit.MaxCallPerSec
		}
		if d.ResourceLimit.CallTimeout > 0 {
			rl.CallTimeout = d.ResourceLimit.CallTimeout
		}
		if d.ResourceLimit.InitTimeout > 0 {
			rl.InitTimeout = d.ResourceLimit.InitTimeout
		}
		if d.ResourceLimit.ResponseSizeMax > 0 {
			rl.ResponseSizeMax = d.ResourceLimit.ResponseSizeMax
		}
		if d.ResourceLimit.WasmSizeMax > 0 {
			rl.WasmSizeMax = d.ResourceLimit.WasmSizeMax
		}
		if d.ResourceLimit.ImportsMax > 0 {
			rl.ImportsMax = d.ResourceLimit.ImportsMax
		}
	}
	return rl
}
