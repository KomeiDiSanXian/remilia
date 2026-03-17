package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBytesToString 测试字节切片到字符串的转换
func TestBytesToString(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "normal string",
			input:    []byte("hello world"),
			expected: "hello world",
		},
		{
			name:     "empty string",
			input:    []byte(""),
			expected: "",
		},
		{
			name:     "chinese characters",
			input:    []byte("你好世界"),
			expected: "你好世界",
		},
		{
			name:     "special characters",
			input:    []byte("!@#$%^&*()"),
			expected: "!@#$%^&*()",
		},
		{
			name:     "numbers",
			input:    []byte("1234567890"),
			expected: "1234567890",
		},
		{
			name:     "mixed content",
			input:    []byte("Test123中文!@#"),
			expected: "Test123中文!@#",
		},
		{
			name:     "newlines and tabs",
			input:    []byte("line1\nline2\ttab"),
			expected: "line1\nline2\ttab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BytesToString(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, string(tt.input), result)
		})
	}
}

// TestStringToBytes 测试字符串到字节切片的转换
func TestStringToBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{name: "normal string", input: "hello world", expected: []byte("hello world")},
		{name: "empty string", input: "", expected: []byte("")},
		{name: "chinese characters", input: "你好世界", expected: []byte("你好世界")},
		{name: "special characters", input: "!@#$%^&*()", expected: []byte("!@#$%^&*()")},
		{name: "numbers", input: "1234567890", expected: []byte("1234567890")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StringToBytes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestBytesToString_StringToBytes_RoundTrip 测试往返转换
func TestBytesToString_StringToBytes_RoundTrip(t *testing.T) {
	tests := []string{
		"", "a", "hello", "Hello World!", "你好世界",
		"Mixed English 123", "Special !@#$%^&*()",
	}

	for _, original := range tests {
		t.Run(original, func(t *testing.T) {
			bytes := StringToBytes(original)
			backToString := BytesToString(bytes)
			assert.Equal(t, original, backToString)
		})
	}
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

// TestFNVHash 测试 FNV 哈希函数
func TestFNVHash(t *testing.T) {
	tests := []string{"hello", "", "long string test", "你好世界", "123456"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			hash := FNVHash(input)
			assert.NotEmpty(t, hash)
			assert.Regexp(t, "^[0-9a-f]+$", hash)
			assert.LessOrEqual(t, len(hash), 16)
		})
	}
}

// TestFNVHash_Consistency 测试哈希一致性
func TestFNVHash_Consistency(t *testing.T) {
	tests := []string{"", "test", "hello world", "你好世界"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			hash1 := FNVHash(input)
			hash2 := FNVHash(input)
			hash3 := FNVHash(input)
			assert.Equal(t, hash1, hash2)
			assert.Equal(t, hash2, hash3)
		})
	}
}

// TestFNVHash_Uniqueness 测试哈希唯一性
func TestFNVHash_Uniqueness(t *testing.T) {
	inputs := []string{"", "a", "b", "aa", "ab", "test1", "test2", "hello", "world"}
	hashes := make(map[string]string)

	for _, input := range inputs {
		hash := FNVHash(input)
		if existingInput, exists := hashes[hash]; exists {
			t.Errorf("Hash collision: %q and %q have same hash %s", input, existingInput, hash)
		}
		hashes[hash] = input
	}

	assert.Equal(t, len(inputs), len(hashes))
}
