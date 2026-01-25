package engine

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestMatcherCompilerBasic 测试基本编译功能
func TestMatcherCompilerBasic(t *testing.T) {
	compiler := NewMatcherCompiler()

	// 创建一个简单的 matcher
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []context.Rule{
			func(ctx *context.Context) bool {
				return true // 简化测试，直接返回 true
			},
		},
		Handler: func(ctx *context.Context) error {
			return nil
		},
		Source:   "test",
		priority: 10,
	}

	// 编译
	compiled := compiler.Compile(matcher)

	if compiled == nil {
		t.Fatal("Compiled matcher is nil")
	}

	if compiled.EventType != dto.C2CMessageCreate {
		t.Errorf("Expected C2CMessageCreate, got %v", compiled.EventType)
	}

	if len(compiled.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(compiled.Rules))
	}

	// 测试缓存
	compiled2 := compiler.Compile(matcher)
	if compiled != compiled2 {
		t.Error("Expected same compiled matcher from cache")
	}
}

// TestCompiledMatcherMatch 测试编译后的匹配器匹配功能
func TestCompiledMatcherMatch(t *testing.T) {
	compiler := NewMatcherCompiler()

	testValue := ""
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []context.Rule{
			func(ctx *context.Context) bool {
				return testValue == "hello"
			},
		},
		Handler:  func(ctx *context.Context) error { return nil },
		Source:   "test",
		priority: 10,
	}

	compiled := compiler.Compile(matcher)

	// 测试匹配
	event := &dto.Payload{
		ID:   "test-1",
		Type: dto.C2CMessageCreate,
	}
	ctx := context.NewContext(event, nil)

	testValue = "hello"
	if !compiled.Match(ctx) {
		t.Error("Expected match for 'hello'")
	}

	// 测试不匹配
	ctx2 := context.NewContext(event, nil)
	testValue = "world"

	if compiled.Match(ctx2) {
		t.Error("Expected no match for 'world'")
	}
}

// TestMatcherCompilerRuleSorting 测试规则按成本排序
func TestMatcherCompilerRuleSorting(t *testing.T) {
	compiler := NewMatcherCompiler()

	rules := []CompiledRule{
		{Cost: 50, Type: "expensive"},
		{Cost: 10, Type: "cheap"},
		{Cost: 30, Type: "medium"},
	}

	compiler.sortRulesByCost(rules)

	// 验证排序结果
	if rules[0].Cost != 10 {
		t.Errorf("Expected first rule cost 10, got %d", rules[0].Cost)
	}
	if rules[1].Cost != 30 {
		t.Errorf("Expected second rule cost 30, got %d", rules[1].Cost)
	}
	if rules[2].Cost != 50 {
		t.Errorf("Expected third rule cost 50, got %d", rules[2].Cost)
	}
}

// TestMatcherCacheGetOrCompile 测试缓存的获取或编译功能
func TestMatcherCacheGetOrCompile(t *testing.T) {
	cache := NewMatcherCache()

	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []context.Rule{
			func(ctx *context.Context) bool { return true },
		},
		Handler:  func(ctx *context.Context) error { return nil },
		Source:   "test",
		priority: 10,
	}

	// 第一次获取
	compiled1 := cache.GetOrCompile(matcher)
	if compiled1 == nil {
		t.Fatal("Compiled matcher is nil")
	}

	// 第二次获取（应该从缓存）
	compiled2 := cache.GetOrCompile(matcher)
	if compiled1 != compiled2 {
		t.Error("Expected same compiled matcher from cache")
	}
}

// TestMatcherCacheGetCompiledMatchers 测试批量编译
func TestMatcherCacheGetCompiledMatchers(t *testing.T) {
	cache := NewMatcherCache()

	matchers := []*Matcher{
		{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test1",
			priority:  10,
		},
		{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test2",
			priority:  20,
		},
	}

	// 批量编译
	compiled := cache.GetCompiledMatchers(dto.C2CMessageCreate, matchers)

	if len(compiled) != 2 {
		t.Errorf("Expected 2 compiled matchers, got %d", len(compiled))
	}

	// 再次获取（应该从缓存）
	compiled2 := cache.GetCompiledMatchers(dto.C2CMessageCreate, matchers)
	if len(compiled2) != 2 {
		t.Errorf("Expected 2 compiled matchers from cache, got %d", len(compiled2))
	}
}

// TestMatcherCacheInvalidate 测试缓存失效
func TestMatcherCacheInvalidate(t *testing.T) {
	cache := NewMatcherCache()

	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test",
		priority:  10,
	}

	// 编译
	compiled1 := cache.GetOrCompile(matcher)

	// 使缓存失效
	cache.compiler.Invalidate(matcher)

	// 重新编译（应该是新对象）
	compiled2 := cache.GetOrCompile(matcher)

	if compiled1 == compiled2 {
		t.Error("Expected different compiled matcher after invalidation")
	}
}

// TestMatcherCacheStats 测试统计信息
func TestMatcherCacheStats(t *testing.T) {
	cache := NewMatcherCache()

	matchers := []*Matcher{
		{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test1",
		},
		{
			EventType: dto.GroupAtMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test2",
		},
	}

	// 编译不同事件类型
	cache.GetCompiledMatchers(dto.C2CMessageCreate, matchers[:1])
	cache.GetCompiledMatchers(dto.GroupAtMessageCreate, matchers[1:])

	stats := cache.Stats()

	if stats.EventTypeCount != 2 {
		t.Errorf("Expected 2 event types, got %d", stats.EventTypeCount)
	}

	if stats.TotalCompiledMatchers != 2 {
		t.Errorf("Expected 2 compiled matchers, got %d", stats.TotalCompiledMatchers)
	}

	t.Logf("Cache stats: %+v", stats)
}

// BenchmarkMatcherCompile 基准测试：编译性能
func BenchmarkMatcherCompile(b *testing.B) {
	compiler := NewMatcherCompiler()

	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []context.Rule{
			func(ctx *context.Context) bool { return true },
			func(ctx *context.Context) bool { return ctx.GetEvent() != nil },
		},
		Handler:  func(ctx *context.Context) error { return nil },
		Source:   "test",
		priority: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiler.Compile(matcher)
	}
}

// BenchmarkCompiledMatcherMatch 基准测试：编译后的匹配性能
func BenchmarkCompiledMatcherMatch(b *testing.B) {
	compiler := NewMatcherCompiler()

	testVal := "hello"
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []context.Rule{
			func(ctx *context.Context) bool { return true },
			func(ctx *context.Context) bool { return testVal == "hello" },
		},
		Handler:  func(ctx *context.Context) error { return nil },
		Source:   "test",
		priority: 10,
	}

	compiled := compiler.Compile(matcher)

	event := &dto.Payload{
		ID:   "test",
		Type: dto.C2CMessageCreate,
	}
	ctx := context.NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled.Match(ctx)
	}
}
