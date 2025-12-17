package remilia

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BenchmarkEngineProcessEvent 测试单个事件处理的性能
func BenchmarkEngineProcessEvent(b *testing.B) {
	engine := NewEngine()
	// 使用新的 OnC2C 快捷方法注册 C2C 事件处理器
	engine.OnC2C().Handle(func(ctx *Context) {
		// 模拟简单处理
		_ = ctx.GetMessageContent()
	})

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}
}

// BenchmarkEngineProcessEventWithMultipleMatchers 测试多个匹配器的性能
func BenchmarkEngineProcessEventWithMultipleMatchers(b *testing.B) {
	engine := NewEngine()

	// 添加10个匹配器，使用 OnC2C 快捷方法
	for i := 0; i < 10; i++ {
		engine.OnC2C().Handle(func(ctx *Context) {
			_ = ctx.GetMessageContent()
		})
	}

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}
}

// BenchmarkEngineProcessEventWithComplexRules 测试复杂规则的性能
func BenchmarkEngineProcessEventWithComplexRules(b *testing.B) {
	engine := NewEngine()

	engine.OnC2C(
		And(
			Or(OnKeyword("hello"), OnKeyword("hi"), OnKeyword("hey")),
			Not(OnSuffix("!")),
		),
	).Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})

	detailMap := map[string]interface{}{"content": "hello world"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}
}

// BenchmarkEngineProcessEventParallel 测试并发事件处理性能
func BenchmarkEngineProcessEventParallel(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := NewContext(event, nil)
			engine.ProcessEvent(ctx)
		}
	})
}

// BenchmarkEngineWithMiddleware 测试带中间件的性能
func BenchmarkEngineWithMiddleware(b *testing.B) {
	engine := NewEngine()

	// 添加两个中间件
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	})

	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	})

	engine.OnC2C().Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}
}

// BenchmarkMatcherMatch 测试匹配器匹配性能
func BenchmarkMatcherMatch(b *testing.B) {
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []Rule{
			OnKeyword("test"),
			OnPrefix("hello"),
		},
	}

	detailMap := map[string]interface{}{"content": "hello test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = matcher.Match(ctx)
	}
}

// BenchmarkContextGetMessageContent 测试消息内容提取性能
func BenchmarkContextGetMessageContent(b *testing.B) {
	detailMap := map[string]interface{}{"content": "test message content"}
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

// BenchmarkRuleOnKeyword 测试关键词规则性能
func BenchmarkRuleOnKeyword(b *testing.B) {
	rule := OnKeyword("test")

	detailMap := map[string]interface{}{"content": "this is a test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rule(ctx)
	}
}

// BenchmarkRuleComplexCombination 测试复杂规则组合性能
func BenchmarkRuleComplexCombination(b *testing.B) {
	rule := And(
		OnC2CMessage(),
		Or(
			OnKeyword("hello"),
			OnCommand("/start"),
			OnPrefix("!"),
		),
		Not(OnSuffix("?")),
	)

	detailMap := map[string]interface{}{"content": "hello world"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rule(ctx)
	}
}
