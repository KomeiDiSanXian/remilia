package helper

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BenchmarkBytesToString 测试 BytesToString 性能
func BenchmarkBytesToString(b *testing.B) {
	data := []byte("test string for benchmark")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BytesToString(data)
	}
}

// BenchmarkBytesToString_vs_Standard 对比性能
func BenchmarkBytesToString_vs_Standard(b *testing.B) {
	data := []byte("test string for comparison")

	b.Run("unsafe", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = BytesToString(data)
		}
	})

	b.Run("standard", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = string(data)
		}
	})
}

// BenchmarkStringToBytes 测试 StringToBytes 性能
func BenchmarkStringToBytes(b *testing.B) {
	data := "test string for benchmark"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = StringToBytes(data)
	}
}

// BenchmarkStringToBytes_vs_Standard 对比性能
func BenchmarkStringToBytes_vs_Standard(b *testing.B) {
	data := "test string for comparison"

	b.Run("unsafe", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = StringToBytes(data)
		}
	})

	b.Run("standard", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = []byte(data)
		}
	})
}

// BenchmarkHideURL 测试 HideURL 性能
func BenchmarkHideURL(b *testing.B) {
	tests := []struct {
		name string
		url  string
	}{
		{"simple", "https://example.com"},
		{"with_path", "https://example.com/path/to/resource"},
		{"with_query", "https://example.com/search?q=test"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = HideURL(tt.url)
			}
		})
	}
}

// BenchmarkFNVHash 测试 FNVHash 性能
func BenchmarkFNVHash(b *testing.B) {
	tests := []struct {
		name string
		data string
	}{
		{"small", "hello"},
		{"medium", strings.Repeat("test", 100)},
		{"large", strings.Repeat("data", 1000)},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = FNVHash(tt.data)
			}
		})
	}
}

// BenchmarkParseEvent 测试 ParseEvent 性能
func BenchmarkParseEvent(b *testing.B) {
	type TestEvent struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}

	jsonData, _ := json.Marshal(map[string]interface{}{
		"id": "test-123", "type": "message", "content": "Hello",
	})
	payload := &dto.Payload{Detail: jsonData}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ParseEvent[TestEvent](payload)
	}
}

// Example functions
func ExampleBytesToString() {
	bytes := []byte("Hello, World!")
	str := BytesToString(bytes)
	fmt.Println(str)
	// Output: Hello, World!
}

func ExampleStringToBytes() {
	str := "Hello, World!"
	bytes := StringToBytes(str)
	fmt.Printf("%d bytes\n", len(bytes))
	// Output: 13 bytes
}

func ExampleHideURL() {
	url := "https://example.com/secret"
	hidden := HideURL(url)
	fmt.Println(hidden)
	// Output: 🔒example点com/secret
}
