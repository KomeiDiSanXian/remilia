// Package wasm 提供基于 wazero 的 WASM 插件运行时，
// 允许第三方插件以 .wasm 模块形式在沙箱中运行。
//
// ABI 约定（v2）：
//   - 数据通过 WASM 线性内存传递，**TLV** 序列化（参见 tlv.go）
//   - WASM 导出函数 plugin_handle 接收 (ptr, len) 两个 i32 参数
//   - WASM 导出函数 plugin_handle 返回 (ptr, len) ——
//     优先检测 wazero 多值返回（results 长度 ≥2），
//     回退到 i64 编码（高32位=len, 低32位=ptr）
//   - WASM 模块可选择导出 malloc(size) 用于宿主分配内存，
//     未导出 malloc 时宿主使用固定通信区偏移 0-65535
package wasm

import "time"

// CurrentABIVersion ABI 版本 — 插件通过导出 plugin_abi_version 声明兼容性
const CurrentABIVersion int32 = 2

// Export names — WASM 模块导出的函数（plugin_init / plugin_handle 为必需）
const (
	ExportInit       = "plugin_init"
	ExportHandle     = "plugin_handle"
	ExportMalloc     = "malloc"
	ExportABIVersion = "plugin_abi_version" // 可选，返回 i32 版本号
	ExportMetadata   = "plugin_metadata"    // 可选，返回 (ptr,len) TLV 元数据
)

// Host module and function names — 宿主导入的模块和函数名
const (
	HostModuleName        = "remilia_host"
	HostFuncLog           = "log"
	HostFuncRegisterCmd   = "register_command"
	HostFuncSendMessage   = "send_message"
	HostFuncGetConfig     = "get_config"
	HostFuncStorageGet    = "storage_get"
	HostFuncStorageSet    = "storage_set"
	HostFuncHTTPRequest   = "http_request"
	HostFuncListFunctions = "__host_functions"   // 返回宿主可用函数列表（TLV）
	HostFuncABIVersion    = "__host_abi_version" // 返回宿主 ABI 版本（i32）
)

// EncodeResult 将 ptr 和 len 编码成单个 uint64 以兼容不支持多值返回的 WASM 版本
func EncodeResult(ptr, len uint32) uint64 {
	return uint64(len)<<32 | uint64(ptr)
}

// DecodeResult 从单个 uint64 解码出 ptr 和 len，兼容不支持多值返回的 WASM 版本
func DecodeResult(v uint64) (ptr, len uint32) {
	return uint32(v), uint32(v >> 32)
}

// 资源限制和安全阈值默认值
// 通过 Descriptor.ResourceLimit 可逐字段覆盖。
const (
	DefaultMemoryPages     = 2 // 128KB
	DefaultMaxCallPerSec   = 1000
	DefaultCallTimeout     = 30 * time.Second
	DefaultCallInitTimeout = 10 * time.Second
	DefaultResponseSizeMax = 1 << 20  // 1MB
	DefaultWasmSizeMax     = 50 << 20 // 50MB
	DefaultImportsMax      = 100
)

// 通信区偏移 — malloc 不可用时使用固定偏移
const (
	CommAreaOffset = 0
	CommAreaSize   = 65536 // 64KB
)
