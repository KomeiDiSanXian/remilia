// Package bytesconv 提供 string 与 []byte 之间的零拷贝转换，以及字符串哈希。
//
// 本包不依赖仓库内任何其他包，属于最底层的工具包，可被 platform、infra 等
// 各层安全引用而不引入循环依赖。
//
// 设计取舍：这里只放「无法再拆分、且被底层组件需要」的原语。业务相关的
// 字符串处理（如 URL 脱敏、事件取值）不放这里。
package bytesconv

import (
	"hash/fnv"
	"strconv"
	"unsafe"
)

// BytesToString 以零拷贝方式把 []byte 转为 string。
//
// 返回的 string 与 b 共享底层内存，因此 b 在结果存活期内不可被修改；
// 若调用方后续会写入 b，请改用 string(b)。
func BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// StringToBytes 以零拷贝方式把 string 转为只读 []byte。
//
// 使用 Go 1.20+ 的 unsafe.Slice + unsafe.StringData，正确处理 data 与 len，
// 不再依赖 *(*[]byte)(unsafe.Pointer(&s))——后者会把 string 结构体后面的
// 任意内存读作 cap 字段，属于未定义行为。
//
// 注意：返回的 []byte 不可写入，其底层指向 string 的只读内存。
// 若需要可修改的副本，请使用 []byte(s)。
func StringToBytes(s string) []byte {
	if len(s) == 0 {
		return []byte{}
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// FNVHash 计算字符串的 FNV-1a 哈希值，返回十六进制字符串。
func FNVHash(s string) string {
	h := fnv.New64a()
	_, _ = h.Write(StringToBytes(s))
	return strconv.FormatUint(h.Sum64(), 16)
}
