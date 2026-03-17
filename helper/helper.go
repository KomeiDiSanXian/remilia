package helper

import (
	"hash/fnv"
	"strconv"
	"strings"
	"unsafe"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// BytesToString  unsafe 零拷贝转换, b不能被修改
func BytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// StringToBytes unsafe 零拷贝将 string 转为只读 []byte。
// 使用 Go 1.20+ unsafe.Slice + unsafe.StringData，正确设置 data 和 len，
// 不再依赖 *(*[]byte)(unsafe.Pointer(&s))——后者会将 string 结构体后面的
// 任意内存误读为 cap 字段，属于未定义行为。
//
// 注意：返回的 []byte 不可写入，其底层指向 string 的只读内存。
// 若需要可修改的副本，请使用 []byte(s)。
func StringToBytes(s string) []byte {
	if len(s) == 0 {
		return []byte{}
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// HideURL 隐藏URL
func HideURL(url string) string {
	url = strings.ReplaceAll(url, "https://", "🔒")
	url = strings.ReplaceAll(url, "http://", "📄")
	url = strings.ReplaceAll(url, ".", "点")
	return url
}

// FNVHash 计算字符串的FNV-1a哈希值并返回十六进制字符串
func FNVHash(s string) string {
	h := fnv.New64a()
	_, _ = h.Write(StringToBytes(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

// ExtractContent 从平台无关事件中提取消息文本内容。
func ExtractContent(e platform.Event) string {
	if e == nil {
		return ""
	}
	return e.Content()
}

// ExtractSenderID 从平台无关事件中提取发送者 ID。
func ExtractSenderID(e platform.Event) string {
	if e == nil {
		return ""
	}
	return e.Sender().ID
}

// ExtractChatID 从平台无关事件中提取会话 ID。
func ExtractChatID(e platform.Event) string {
	if e == nil {
		return ""
	}
	return e.Chat().ID
}
