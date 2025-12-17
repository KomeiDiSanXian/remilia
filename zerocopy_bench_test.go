package remilia

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/tidwall/gjson"
)

// BenchmarkGetMessageContentJSON 使用传统 JSON 解析
func BenchmarkGetMessageContentJSON(b *testing.B) {
	detailMap := map[string]interface{}{
		"content":   "Hello World, this is a test message",
		"id":        "123456789",
		"timestamp": 1234567890,
		"extra":     "some extra data",
	}
	detailJSON, _ := json.Marshal(detailMap)

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 模拟旧实现
		var detail map[string]any
		json.Unmarshal(event.Detail, &detail)
		_ = detail["content"].(string)
	}
}

// BenchmarkGetMessageContentGjson 使用 gjson 零拷贝
func BenchmarkGetMessageContentGjson(b *testing.B) {
	detailMap := map[string]interface{}{
		"content":   "Hello World, this is a test message",
		"id":        "123456789",
		"timestamp": 1234567890,
		"extra":     "some extra data",
	}
	detailJSON, _ := json.Marshal(detailMap)

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := gjson.GetBytes(event.Detail, "content")
		_ = result.String()
	}
}

// BenchmarkGetMessageContentCurrent 当前优化后的实现
func BenchmarkGetMessageContentCurrent(b *testing.B) {
	detailMap := map[string]interface{}{
		"content":   "Hello World, this is a test message",
		"id":        "123456789",
		"timestamp": 1234567890,
		"extra":     "some extra data",
	}
	detailJSON, _ := json.Marshal(detailMap)

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.GetMessageContent()
	}
}

// BenchmarkGetMessageContentSizes 不同消息大小的性能
func BenchmarkGetMessageContentSizes(b *testing.B) {
	sizes := []struct {
		name    string
		content string
	}{
		{"Small-10B", "Hello"},
		{"Medium-100B", "Hello World, this is a test message with some more content to make it longer"},
		{"Large-1KB", string(make([]byte, 1024))},
	}

	for _, size := range sizes {
		detailMap := map[string]interface{}{
			"content":   size.content,
			"id":        "123456789",
			"timestamp": 1234567890,
		}
		detailJSON, _ := json.Marshal(detailMap)
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailJSON,
		}

		b.Run(size.name+"-JSON", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var detail map[string]any
				json.Unmarshal(event.Detail, &detail)
				_ = detail["content"].(string)
			}
		})

		b.Run(size.name+"-Gjson", func(b *testing.B) {
			ctx := NewContext(event, nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = ctx.GetMessageContent()
			}
		})
	}
}

// BenchmarkRuleMatchingJSON 规则匹配场景（JSON）
func BenchmarkRuleMatchingJSON(b *testing.B) {
	detailMap := map[string]interface{}{
		"content": "Hello World 123",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	// 模拟旧实现的规则
	ruleOld := func() bool {
		var detail map[string]any
		json.Unmarshal(event.Detail, &detail)
		content := detail["content"].(string)
		// 简单的包含检查
		return len(content) > 0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ruleOld()
	}
}

// BenchmarkRuleMatchingGjson 规则匹配场景（gjson）
func BenchmarkRuleMatchingGjson(b *testing.B) {
	detailMap := map[string]interface{}{
		"content": "Hello World 123",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	// 现在的实现
	rule := func() bool {
		content := ctx.GetMessageContent()
		return len(content) > 0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rule()
	}
}

// BenchmarkMultipleFieldAccess 多次字段访问
func BenchmarkMultipleFieldAccess(b *testing.B) {
	detailMap := map[string]interface{}{
		"content":   "Hello World",
		"id":        "123456789",
		"timestamp": 1234567890,
	}
	detailJSON, _ := json.Marshal(detailMap)

	b.Run("JSON-3Fields", func(b *testing.B) {
		event := &dto.Payload{Detail: detailJSON}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// 每次都要解析整个 JSON
			var detail map[string]any
			json.Unmarshal(event.Detail, &detail)
			_ = detail["content"].(string)

			json.Unmarshal(event.Detail, &detail)
			_ = detail["id"].(string)

			json.Unmarshal(event.Detail, &detail)
			_ = detail["timestamp"].(float64)
		}
	})

	b.Run("Gjson-3Fields", func(b *testing.B) {
		event := &dto.Payload{Detail: detailJSON}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// 每次只提取需要的字段
			_ = gjson.GetBytes(event.Detail, "content").String()
			_ = gjson.GetBytes(event.Detail, "id").String()
			_ = gjson.GetBytes(event.Detail, "timestamp").Int()
		}
	})
}

// BenchmarkGetAuthorJSON 传统方式获取作者
func BenchmarkGetAuthorJSON(b *testing.B) {
	detailMap := map[string]interface{}{
		"content": "test",
		"author": map[string]interface{}{
			"id":       "user123",
			"username": "testuser",
		},
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var detail map[string]any
		json.Unmarshal(event.Detail, &detail)
		_ = detail["author"]
	}
}

// BenchmarkGetAuthorGjson 使用 gjson 获取作者
func BenchmarkGetAuthorGjson(b *testing.B) {
	detailMap := map[string]interface{}{
		"content": "test",
		"author": map[string]interface{}{
			"id":       "user123",
			"username": "testuser",
		},
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.GetAuthor()
	}
}

// BenchmarkComplexJSONParsing 复杂 JSON 解析对比
func BenchmarkComplexJSONParsing(b *testing.B) {
	// 模拟复杂的消息结构
	detailMap := map[string]interface{}{
		"content": "test message",
		"author": map[string]interface{}{
			"id":       "user123",
			"username": "testuser",
		},
		"mentions": []interface{}{
			map[string]interface{}{"id": "u1", "username": "user1"},
			map[string]interface{}{"id": "u2", "username": "user2"},
		},
		"attachments": []interface{}{
			map[string]interface{}{"type": "image", "url": "http://example.com/1.jpg"},
			map[string]interface{}{"type": "file", "url": "http://example.com/2.pdf"},
		},
		"metadata": map[string]interface{}{
			"timestamp": 1234567890,
			"channel":   "general",
		},
	}
	detailJSON, _ := json.Marshal(detailMap)

	b.Run("JSON-FullParse", func(b *testing.B) {
		event := &dto.Payload{Detail: detailJSON}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var detail map[string]any
			json.Unmarshal(event.Detail, &detail)
			_ = detail["content"].(string)
		}
	})

	b.Run("Gjson-FieldOnly", func(b *testing.B) {
		event := &dto.Payload{Detail: detailJSON}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result := gjson.GetBytes(event.Detail, "content")
			_ = result.String()
		}
	})
}
