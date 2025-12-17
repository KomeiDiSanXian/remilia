package remilia

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BenchmarkRegexPrecompiled 预编译正则性能基准
func BenchmarkRegexPrecompiled(b *testing.B) {
	rule := OnRegex(`\d+`)
	detailMap := map[string]interface{}{
		"content": "测试消息123",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rule(ctx)
	}
}

// BenchmarkRegexNotPrecompiled 不预编译的基准（模拟）
func BenchmarkRegexNotPrecompiled(b *testing.B) {
	pattern := `\d+`
	detailMap := map[string]interface{}{
		"content": "测试消息123",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 每次都编译（模拟不预编译的情况）
		re := regexp.MustCompile(pattern)
		content := ctx.GetMessageContent()
		re.MatchString(content)
	}
}

// BenchmarkRegexComplexity 不同复杂度正则的性能
func BenchmarkRegexComplexity(b *testing.B) {
	tests := []struct {
		name    string
		pattern string
		message string
	}{
		{
			name:    "Simple-Digit",
			pattern: `\d+`,
			message: "测试123",
		},
		{
			name:    "Medium-Email",
			pattern: `^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$`,
			message: "user@example.com",
		},
		{
			name:    "Complex-URL",
			pattern: `https?://(?:www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b(?:[-a-zA-Z0-9()@:%_\+.~#?&/=]*)`,
			message: "https://www.example.com/path?query=value",
		},
	}

	for _, tt := range tests {
		detailMap := map[string]interface{}{
			"content": tt.message,
		}
		detailJSON, _ := json.Marshal(detailMap)
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailJSON,
		}

		b.Run(tt.name+"-Precompiled", func(b *testing.B) {
			rule := OnRegex(tt.pattern)
			ctx := NewContext(event, nil)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rule(ctx)
			}
		})

		b.Run(tt.name+"-NotPrecompiled", func(b *testing.B) {
			ctx := NewContext(event, nil)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				re := regexp.MustCompile(tt.pattern)
				content := ctx.GetMessageContent()
				re.MatchString(content)
			}
		})
	}
}

// BenchmarkRegexVsStringOperations 正则 vs 字符串操作性能对比
func BenchmarkRegexVsStringOperations(b *testing.B) {
	message := "hello world 123"
	detailMap := map[string]interface{}{
		"content": message,
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.Run("Regex-Contains", func(b *testing.B) {
		rule := OnRegex(`world`)
		ctx := NewContext(event, nil)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rule(ctx)
		}
	})

	b.Run("String-Contains", func(b *testing.B) {
		rule := OnKeyword("world")
		ctx := NewContext(event, nil)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rule(ctx)
		}
	})

	b.Run("Regex-Prefix", func(b *testing.B) {
		rule := OnRegex(`^hello`)
		ctx := NewContext(event, nil)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rule(ctx)
		}
	})

	b.Run("String-Prefix", func(b *testing.B) {
		rule := OnPrefix("hello")
		ctx := NewContext(event, nil)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rule(ctx)
		}
	})
}
