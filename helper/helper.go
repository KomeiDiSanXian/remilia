package helper

import (
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/bytesconv"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// BytesToString 以零拷贝方式把 []byte 转为 string。
//
// Deprecated: 使用 infra/bytesconv.BytesToString。本包保留该转发仅为兼容，
// 新代码请直接引用 infra/bytesconv，以免底层包反向依赖 helper。
func BytesToString(b []byte) string {
	return bytesconv.BytesToString(b)
}

// StringToBytes 以零拷贝方式把 string 转为只读 []byte。
//
// Deprecated: 使用 infra/bytesconv.StringToBytes。
func StringToBytes(s string) []byte {
	return bytesconv.StringToBytes(s)
}

// HideURL 隐藏URL
func HideURL(url string) string {
	url = strings.ReplaceAll(url, "https://", "🔒")
	url = strings.ReplaceAll(url, "http://", "📄")
	url = strings.ReplaceAll(url, ".", "点")
	return url
}

// FNVHash 计算字符串的FNV-1a哈希值并返回十六进制字符串。
//
// Deprecated: 使用 infra/bytesconv.FNVHash。
func FNVHash(s string) string {
	return bytesconv.FNVHash(s)
}

// ExtractContent 从平台无关事件中提取消息文本内容。
//
// Deprecated: 直接使用 platform.Content(e)，本函数只是其 nil 安全包装。
func ExtractContent(e platform.Event) string {
	if e == nil {
		return ""
	}
	return platform.Content(e)
}

// ExtractSenderID 从平台无关事件中提取发送者 ID。
//
// Deprecated: 直接使用 e.Sender().ID，本函数只是其 nil 安全包装。
func ExtractSenderID(e platform.Event) string {
	if e == nil {
		return ""
	}
	return e.Sender().ID
}

// ExtractChatID 从平台无关事件中提取会话 ID。
//
// Deprecated: 直接使用 e.Chat().ID，本函数只是其 nil 安全包装。
func ExtractChatID(e platform.Event) string {
	if e == nil {
		return ""
	}
	return e.Chat().ID
}
