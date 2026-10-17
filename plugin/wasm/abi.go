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
	ExportFree       = "free"               // 可选，宿主用于释放通过 malloc 分配的请求缓冲
	ExportABIVersion = "plugin_abi_version" // 可选，返回 i32 版本号
	ExportMetadata   = "plugin_metadata"    // 可选，返回 (ptr,len) TLV 元数据
)

// Host module and function names — 宿主导入的模块和函数名
const (
	HostModuleName = "remilia_host"

	// ── 框架默认提供（RegisterDefaultHostFuncs）────────────────────────────
	//
	// 只有这三个（外加两个自描述函数）在 NewRuntime 后必定可用：
	//
	//	HostFuncLog           写日志（TLV: level/message）
	//	HostFuncGetConfig     读取插件配置（TLV: m=模块名, k=键；由 Runtime 注入实现）
	//	HostFuncABIVersion    宿主 ABI 版本（自描述）
	//	HostFuncListFunctions 宿主可用函数列表（自描述，插件用来探测能力）
	//
	// 宿主函数是 wazero 的模块导入：**模块导入未提供的函数会导致实例化失败**。
	// 因此插件不应硬编码导入下面的可选函数，而应先调用 HostFuncListFunctions
	// 探测（或由宿主应用按需注册后再加载模块）。
	HostFuncLog           = "log"
	HostFuncGetConfig     = "get_config"
	HostFuncListFunctions = "__host_functions"   // 返回宿主可用函数列表（TLV）
	HostFuncABIVersion    = "__host_abi_version" // 返回宿主 ABI 版本（i32）

	// ── 约定名（可选，由宿主应用通过 HostFuncRegistry.Register 提供）───────
	//
	// 框架不内置这些实现：它们需要宿主应用的业务依赖（命令注册、
	// 平台发送、存储、HTTP 客户端与出站策略）。名字在此固定，避免宿主与
	// 插件各自发明导致 ABI 漂移。
	HostFuncRegisterCmd = "register_command" // 注册命令（宿主侧需要 Bridge/Engine）
	HostFuncSendMessage = "send_message"     // 主动发送消息（需要平台 Sender）
	HostFuncStorageGet  = "storage_get"      // 读取键值存储（需要存储后端）
	HostFuncStorageSet  = "storage_set"      // 写入键值存储
	HostFuncHTTPRequest = "http_request"     // 发起 HTTP 请求（需要客户端与出站策略）
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
	// DefaultMemoryPages 默认内存页上限（每页 64KB）。
	// 自内存限制真正生效（独占 Runtime 传入 WithMemoryLimitPages）起，
	// 旧默认 2 页（128KB）对 TinyGo 模块过小，调整为 256 页（16MB）。
	DefaultMemoryPages     = 256 // 16MB
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
