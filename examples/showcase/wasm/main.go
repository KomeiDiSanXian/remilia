package main

import (
	"strings"
	"unsafe"
)

// ── 应答缓冲区 ────────────────────────────────────────────────────────────────

var respBuf [4096]byte

// ── 插件状态 ──────────────────────────────────────────────────────────────────

var callCounter uint32

// ── 宿主函数导入 ──────────────────────────────────────────────────────────────

//go:wasmimport remilia_host log
func hostLog(ptr uint32, len uint32) uint64

//go:wasmimport remilia_host get_config
func hostGetConfig(ptr uint32, len uint32) uint64

//go:wasmimport remilia_host storage_get
func hostStorageGet(ptr uint32, len uint32) uint64

//go:wasmimport remilia_host storage_set
func hostStorageSet(ptr uint32, len uint32) uint64

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

func writeTLV(key, val string) uint64 {
	// 直接写入 respBuf 避免 TinyGo slice 操作问题
	keyLen := len(key)
	valLen := len(val)
	total := 1 + keyLen + 1 + valLen // ULEB128(keyLen) + key + ULEB128(valLen) + val
	if uint32(total) > uint32(len(respBuf)) {
		return 0
	}
	respBuf[0] = byte(keyLen)        // key length (assumes < 128)
	copy(respBuf[1:], key)           // key
	respBuf[1+keyLen] = byte(valLen) // value length (assumes < 128)
	copy(respBuf[1+keyLen+1:], []byte(val))

	ptr := uint32(uintptr(unsafe.Pointer(&respBuf[0])))
	return uint64(total)<<32 | uint64(ptr)
}

func callHost(name string, args []byte) ([]byte, bool) {
	var argPtr uint32
	argLen := uint32(len(args))
	if argLen > 0 {
		argPtr = uint32(uintptr(unsafe.Pointer(&args[0])))
	}
	var result uint64
	switch name {
	case "log":
		result = hostLog(argPtr, argLen)
	case "get_config":
		result = hostGetConfig(argPtr, argLen)
	case "storage_get":
		result = hostStorageGet(argPtr, argLen)
	case "storage_set":
		result = hostStorageSet(argPtr, argLen)
	}
	resPtr := uint32(result)
	resLen := uint32(result >> 32)
	if resPtr == 0 || resLen == 0 {
		return nil, false
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(resPtr))), resLen)
	out := make([]byte, resLen)
	copy(out, buf)
	return out, true
}

// ── ABI 导出 ──────────────────────────────────────────────────────────────────

//export plugin_abi_version
func pluginABIVersion() int32 {
	return 2
}

//export plugin_init
func pluginInit() int32 {
	callCounter = 0
	return 0
}

//export plugin_handle
func pluginHandle(ptr uint32, length uint32) uint64 {
	if length == 0 {
		return 0
	}
	callCounter++

	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	content := tlvRead(buf, "c")

	switch {
	case content == "/wasmhello":
		return writeTLV("r", "你好！我是 TinyGo WASM 插件！")

	case content == "/wasmping":
		return writeTLV("r", "pong from wasm")

	case content == "/wasmcount":
		return writeTLV("r", formatUint(callCounter))

	case strings.HasPrefix(content, "/wasmecho "):
		text := strings.TrimPrefix(content, "/wasmecho ")
		return writeTLV("r", "echo: "+text)

	case content == "/wasmstore":
		return handleStorage()

	case content == "/wasmhost":
		return handleHostInfo()

	default:
		return writeTLV("r", "收到: "+content)
	}
}

// ── 高级命令处理 ──────────────────────────────────────────────────────────────

func makeTLV(key, val string) []byte {
	b := make([]byte, 0, 2+len(key)+len(val))
	b = append(b, byte(len(key)))
	b = append(b, key...)
	b = append(b, byte(len(val)))
	b = append(b, []byte(val)...)
	return b
}

func handleStorage() uint64 {
	reqGet := makeTLV("k", "demo_key")
	resp, ok := callHost("storage_get", reqGet)
	if !ok {
		return writeTLV("r", "storage_get 返回空")
	}
	val := tlvRead(resp, "v")

	newVal := "count=" + formatUint(callCounter)
	reqSet := makeTLV("k", "demo_key")
	reqSet = append(reqSet, byte(len("v")))
	reqSet = append(reqSet, "v"...)
	reqSet = append(reqSet, byte(len(newVal)))
	reqSet = append(reqSet, []byte(newVal)...)
	callHost("storage_set", reqSet)

	if val == "" {
		return writeTLV("r", "storage 值已设为: "+newVal)
	}
	return writeTLV("r", "storage 原值="+val+" 新值="+newVal)
}

func handleHostInfo() uint64 {
	req := makeTLV("k", "host_info")
	resp, ok := callHost("get_config", req)
	if !ok {
		return writeTLV("r", "get_config 返回空")
	}
	val := tlvRead(resp, "v")
	return writeTLV("r", "宿主信息: "+val)
}

// ── 工具函数 ──────────────────────────────────────────────────────────────────

func formatUint(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func main() {}
