package main

import (
	"unsafe"
)

// ── 应答缓冲区 ────────────────────────────────────────────────────────────────

var respBuf [4096]byte

// ── TLV 键名 ──────────────────────────────────────────────────────────────────
// c = content  r = reply  k = key  v = value  level/message = log params

// ── 宿主函数导入 ──────────────────────────────────────────────────────────────

//go:wasmimport remilia_host log
func hostLog(ptr uint32, len uint32) uint64

// ── TLV 编解码 ────────────────────────────────────────────────────────────────

func tlvRead(data []byte, key string) string {
	i := 0
	for i < len(data) {
		kLen, n := decodeULEB128(data[i:])
		if n == 0 {
			break
		}
		i += n
		if i+int(kLen) > len(data) {
			break
		}
		k := string(data[i : i+int(kLen)])
		i += int(kLen)

		vLen, n := decodeULEB128(data[i:])
		if n == 0 {
			break
		}
		i += n
		if i+int(vLen) > len(data) {
			break
		}
		v := string(data[i : i+int(vLen)])
		i += int(vLen)

		if k == key {
			return v
		}
	}
	return ""
}

func decodeULEB128(data []byte) (uint32, int) {
	var v uint32
	for i := 0; i < len(data); i++ {
		c := data[i]
		v |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}

// ── ABI 导出 ──────────────────────────────────────────────────────────────────

//export plugin_abi_version
func pluginABIVersion() int32 {
	return 2
}

//export plugin_init
func pluginInit() int32 {
	return 0
}

//export plugin_handle
func pluginHandle(ptr uint32, length uint32) uint64 {
	if length == 0 {
		return 0
	}

	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	content := tlvRead(buf, "c")

	var reply string
	switch content {
	case "/wasmhello":
		reply = "你好！我是 TinyGo WASM 插件！"
	case "/wasmping":
		reply = "pong from wasm"
	default:
		reply = "收到: " + content
	}

	// 直接构建 TLV 响应（避免任何中间 slice）
	// TLV: [key_len=1][key='r'][val_len=n][val_bytes...]
	respBuf[0] = 1                // key length: 1
	respBuf[1] = 'r'              // key: "r"
	respBuf[2] = byte(len(reply)) // value length
	copy(respBuf[3:], []byte(reply))
	respLen := uint32(3 + len(reply))

	respPtr := uint32(uintptr(unsafe.Pointer(&respBuf[0])))
	return uint64(respLen)<<32 | uint64(respPtr)
}

func main() {}
