package main

import (
	"encoding/json"
	"unsafe"
)

// ── 宿主函数导入 ──────────────────────────────────────────────────────────────

//go:wasmimport remilia_host log
func hostLog(ptr uint32, len uint32) uint64

//go:wasmimport remilia_host register_command
func hostRegisterCommand(ptr uint32, len uint32) uint64

// ── WASM 堆 ───────────────────────────────────────────────────────────────────
// 使用静态缓冲区作为 WASM 插件堆，宿主通过 malloc 在此分配内存。
// 总大小 128KB，与默认 DefaultMemoryPages 一致。

var wasmHeap [128 * 1024]byte
var wasmHeapIndex uint32

// ── ABI 导出 ──────────────────────────────────────────────────────────────────

//go:wasmexport plugin_init
func pluginInit() int32 {
	wasmHeapIndex = 0
	return 0
}

//go:wasmexport plugin_handle
func pluginHandle(ptr uint32, length uint32) uint64 {
	if length == 0 {
		return 0
	}

	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)

	var event struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(buf, &event); err != nil {
		logHost("error: json unmarshal: " + err.Error())
		return 0
	}

	logHost("handle: " + event.Content)

	var reply string
	switch event.Content {
	case "/wasmhello":
		reply = "你好，我是 WASM 插件！"
	case "/wasmping":
		reply = "pong"
	default:
		reply = "收到: " + event.Content
	}

	resp, err := json.Marshal(map[string]string{"reply": reply})
	if err != nil {
		return 0
	}
	respLen := uint32(len(resp))
	respPtr := malloc(respLen)
	if respPtr == 0 {
		return 0
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(respPtr))), respLen)
	copy(dst, resp)
	return uint64(respLen)<<32 | uint64(respPtr)
}

//go:wasmexport malloc
func malloc(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	size = (size + 3) & ^uint32(3)
	if wasmHeapIndex+size > uint32(len(wasmHeap)) {
		return 0
	}
	ptr := wasmHeapIndex
	wasmHeapIndex += size
	return ptr
}

// ── 辅助函数 ──────────────────────────────────────────────────────────────────

func logHost(msg string) {
	if msg == "" {
		return
	}
	b := []byte(msg)
	hostLog(uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

// ── main 函数（必须，Go wasip1/wasm 需要） ────────────────────────────────────
// _start 会调用 init() 函数后调用 main()。main() 立即返回。

func main() {}
