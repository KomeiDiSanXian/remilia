package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDeprecatedDelegates 验证本包的兼容转发与 infra/bytesconv 行为一致。
//
// 这些函数已标注 Deprecated，实现委托给 infra/bytesconv；此处只做转发正确性
// 校验，原语本身的完整测试位于 infra/bytesconv/bytesconv_test.go。
func TestDeprecatedDelegates(t *testing.T) {
	t.Run("BytesToString", func(t *testing.T) {
		assert.Equal(t, "hello", BytesToString([]byte("hello")))
		assert.Equal(t, "", BytesToString(nil))
		assert.Equal(t, "", BytesToString([]byte("")))
	})

	t.Run("StringToBytes", func(t *testing.T) {
		assert.Equal(t, []byte("hello"), StringToBytes("hello"))
		assert.Empty(t, StringToBytes(""))
	})

	t.Run("round trip", func(t *testing.T) {
		for _, s := range []string{"", "a", "你好世界", "Mixed 123!@#"} {
			assert.Equal(t, s, BytesToString(StringToBytes(s)))
		}
	})

	t.Run("FNVHash", func(t *testing.T) {
		assert.Equal(t, FNVHash("hello"), FNVHash("hello"))
		assert.NotEqual(t, FNVHash("hello"), FNVHash("world"))
		assert.Regexp(t, "^[0-9a-f]+$", FNVHash("你好世界"))
	})
}

// TestHideURL 测试 URL 隐藏功能
func TestHideURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "https URL", input: "https://example.com/path", expected: "🔒example点com/path"},
		{name: "http URL", input: "http://example.com/path", expected: "📄example点com/path"},
		{name: "multiple dots", input: "https://sub.example.com", expected: "🔒sub点example点com"},
		{name: "URL with query", input: "https://example.com/search?q=test", expected: "🔒example点com/search?q=test"},
		{name: "plain domain", input: "example.com", expected: "example点com"},
		{name: "empty string", input: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HideURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtract_NilSafe 验证 nil 事件上的取值包装返回空字符串而不 panic。
func TestExtract_NilSafe(t *testing.T) {
	assert.Equal(t, "", ExtractContent(nil))
	assert.Equal(t, "", ExtractSenderID(nil))
	assert.Equal(t, "", ExtractChatID(nil))
}
