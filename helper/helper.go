package helper

import (
	"hash/fnv"
	"strconv"
	"strings"
	"unsafe"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BytesToString  unsafe 零拷贝转换, b不能被修改
func BytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// StringToBytes unsafe 零拷贝转换
func StringToBytes(s string) (b []byte) { return *(*[]byte)(unsafe.Pointer(&s)) }

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

// ParseEvent 泛型事件解析器
func ParseEvent[T any](p *dto.Payload) (*T, error) {
	var event T
	if err := p.Decode(&event); err != nil {
		return nil, err
	}
	return &event, nil
}
