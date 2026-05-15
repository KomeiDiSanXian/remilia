package wasm

import (
	"fmt"
	"time"
)

// Descriptor 描述一个 WASM 插件的配置。
// 与 [plugin.Descriptor] 不同，Descriptor 不包含 Setup/Teardown 回调，
// 而是通过加载 .wasm 文件并调用其导出的函数来完成生命周期管理。
type Descriptor struct {
	// Name 是插件的唯一标识。
	Name string
	// Version 是插件的版本号（可选，建议 semver）。
	Version string
	// Path 是 .wasm 文件的路径。
	Path string
	// Config 是传递给插件的配置（JSON 序列化后通过 get_config 宿主函数读取）。
	Config map[string]any
	// ResourceLimit 可选资源限制。为 nil 时使用默认值。
	ResourceLimit *ResourceLimit
	// CallTimeout 单次 handle 调用的超时时间。零值表示使用默认值（5s）。
	CallTimeout time.Duration
}

// ResourceLimit 定义 WASM 插件的资源限制。
type ResourceLimit struct {
	// MemoryPages 最大内存页数（每页 64KB）。默认 2（128KB）。
	MemoryPages uint32
	// MaxCallPerSec 每秒最大调用次数。默认 1000。
	MaxCallPerSec int64
}

// Validate 校验 Descriptor 的必填字段。
func (d *Descriptor) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("wasm: Descriptor.Name is required")
	}
	if d.Path == "" {
		return fmt.Errorf("wasm: Descriptor.Path is required")
	}
	return nil
}

// EffectiveResourceLimit 返回生效的资源限制（nil 字段用默认值填充）。
func (d *Descriptor) EffectiveResourceLimit() ResourceLimit {
	rl := ResourceLimit{
		MemoryPages:   DefaultMemoryPages,
		MaxCallPerSec: DefaultMaxCallPerSec,
	}
	if d.ResourceLimit != nil {
		if d.ResourceLimit.MemoryPages > 0 {
			rl.MemoryPages = d.ResourceLimit.MemoryPages
		}
		if d.ResourceLimit.MaxCallPerSec > 0 {
			rl.MaxCallPerSec = d.ResourceLimit.MaxCallPerSec
		}
	}
	return rl
}
