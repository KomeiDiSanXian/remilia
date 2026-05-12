// Package wasm 提供基于 wazero 的 WASM 插件运行时，
// 允许第三方插件以 .wasm 模块形式在沙箱中运行。
//
// ABI 约定：
//   - 数据通过 WASM 线性内存传递，JSON 序列化
//   - WASM 导出函数接收 (ptr, len) 两个 i32 参数
//   - WASM 返回 (ptr, len) 编码在单个 i64 中（高32位=len, 低32位=ptr）
//   - WASM 模块需导出 malloc(size) 用于宿主分配内存
package wasm

// Export names — WASM 模块必须导出的函数
const (
	ExportInit   = "plugin_init"
	ExportHandle = "plugin_handle"
	ExportMalloc = "malloc"
)

// Host module and function names — 宿主导入的模块和函数名
const (
	HostModuleName        = "remilia_host"
	HostFuncLog           = "remilia_host_log"
	HostFuncRegisterCmd   = "remilia_host_register_command"
	HostFuncSendMessage   = "remilia_host_send_message"
	HostFuncGetConfig     = "remilia_host_get_config"
	HostFuncStorageGet    = "remilia_host_storage_get"
	HostFuncStorageSet    = "remilia_host_storage_set"
	HostFuncHTTPRequest   = "remilia_host_http_request"
)

// CallResult 编解码 i64 返回值（高32位=len, 低32位=ptr）。
// ptr=0 表示空结果/错误。
func EncodeResult(ptr, len uint32) uint64 {
	return uint64(len)<<32 | uint64(ptr)
}

func DecodeResult(v uint64) (ptr, len uint32) {
	return uint32(v), uint32(v >> 32)
}

// DefaultResourceLimits 默认资源限制
const (
	DefaultMemoryPages     = 2    // 128KB
	DefaultMaxCallPerSec   = 1000
	DefaultCallTimeoutSec  = 5
)
