package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestEngineMatcherLimit 测试匹配器数量限制功能
func TestEngineMatcherLimit(t *testing.T) {
	engine := NewEngine()

	// 设置限制为 5
	engine.SetMaxMatchers(5)

	// 验证限制已设置
	if engine.GetMaxMatchers() != 5 {
		t.Errorf("Expected max matchers = 5, got %d", engine.GetMaxMatchers())
	}

	// 注册 5 个匹配器，应该都成功
	for i := 0; i < 5; i++ {
		m := engine.On(dto.C2CMessageCreate, OnCommand("test"))
		if m == nil {
			t.Errorf("Expected matcher %d to be created, got nil", i)
		}
	}

	// 验证已注册 5 个
	if engine.GetMatcherCount() != 5 {
		t.Errorf("Expected 5 matchers, got %d", engine.GetMatcherCount())
	}

	// 尝试注册第 6 个，应该返回 noopMatcher
	m6 := engine.On(dto.C2CMessageCreate, OnCommand("test6"))
	if m6 == nil {
		t.Error("Expected noopMatcher, got nil")
	}
	if m6 != noopMatcher {
		t.Error("Expected 6th matcher to be noopMatcher")
	}

	// 验证仍然是 5 个
	if engine.GetMatcherCount() != 5 {
		t.Errorf("Expected 5 matchers after limit reached, got %d", engine.GetMatcherCount())
	}
}

// TestEngineNoMatcherLimit 测试不设置限制时的行为
func TestEngineNoMatcherLimit(t *testing.T) {
	engine := NewEngine()

	// 默认应该是 0（不限制）
	if engine.GetMaxMatchers() != 0 {
		t.Errorf("Expected default max matchers = 0, got %d", engine.GetMaxMatchers())
	}

	// 注册 100 个匹配器，应该都成功
	for i := 0; i < 100; i++ {
		m := engine.On(dto.C2CMessageCreate, OnCommand("test"))
		if m == nil {
			t.Errorf("Expected matcher %d to be created, got nil", i)
		}
	}

	// 验证已注册 100 个
	if engine.GetMatcherCount() != 100 {
		t.Errorf("Expected 100 matchers, got %d", engine.GetMatcherCount())
	}
}

// TestEngineMatcherLimitDeletion 测试删除匹配器后可以继续注册
func TestEngineMatcherLimitDeletion(t *testing.T) {
	engine := NewEngine()
	engine.SetMaxMatchers(3)

	// 注册 3 个匹配器
	m1 := engine.On(dto.C2CMessageCreate, OnCommand("test1"))
	m2 := engine.On(dto.C2CMessageCreate, OnCommand("test2"))
	m3 := engine.On(dto.C2CMessageCreate, OnCommand("test3"))

	if m1 == nil || m2 == nil || m3 == nil {
		t.Fatal("Expected all 3 matchers to be created")
	}

	// 第 4 个应该返回 noopMatcher
	m4 := engine.On(dto.C2CMessageCreate, OnCommand("test4"))
	if m4 != noopMatcher {
		t.Error("Expected 4th matcher to be noopMatcher")
	}

	// 删除一个匹配器
	m2.Delete()

	// 现在应该可以注册新的
	m5 := engine.On(dto.C2CMessageCreate, OnCommand("test5"))
	if m5 == nil {
		t.Error("Expected to be able to register matcher after deletion")
	}

	// 验证总数为 3
	if engine.GetMatcherCount() != 3 {
		t.Errorf("Expected 3 matchers after deletion and new registration, got %d", engine.GetMatcherCount())
	}
}

// TestEngineMatcherLimitZeroMeansUnlimited 测试设置为 0 表示不限制
func TestEngineMatcherLimitZeroMeansUnlimited(t *testing.T) {
	engine := NewEngine()

	// 先设置限制
	engine.SetMaxMatchers(5)

	// 注册 5 个
	for i := 0; i < 5; i++ {
		engine.On(dto.C2CMessageCreate, OnCommand("test"))
	}

	// 第 6 个应该失败，返回 noopMatcher
	m6 := engine.On(dto.C2CMessageCreate, OnCommand("test6"))
	if m6 != noopMatcher {
		t.Error("Expected 6th matcher to be noopMatcher with limit=5")
	}

	// 设置为 0（取消限制）
	engine.SetMaxMatchers(0)

	// 现在应该可以注册
	m7 := engine.On(dto.C2CMessageCreate, OnCommand("test7"))
	if m7 == nil {
		t.Error("Expected matcher to be registered after removing limit")
	}

	// 应该有 6 个
	if engine.GetMatcherCount() != 6 {
		t.Errorf("Expected 6 matchers, got %d", engine.GetMatcherCount())
	}
}

// TestEngineMatcherLimitChainCall 测试链式调用
func TestEngineMatcherLimitChainCall(t *testing.T) {
	engine := NewEngine().
		SetMaxMatchers(10)

	if engine.GetMaxMatchers() != 10 {
		t.Errorf("Expected max matchers = 10, got %d", engine.GetMaxMatchers())
	}

	// 验证链式调用返回正确的引擎实例
	engine2 := engine.SetMaxMatchers(20)
	if engine2 != engine {
		t.Error("Expected SetMaxMatchers to return the same engine instance")
	}
}

// TestEngineMatcherLimitConcurrent 测试并发注册时的限制
func TestEngineMatcherLimitConcurrent(t *testing.T) {
	engine := NewEngine()
	engine.SetMaxMatchers(50)

	// 并发注册 100 个匹配器
	results := make(chan *Matcher, 100)
	for i := 0; i < 100; i++ {
		go func() {
			m := engine.On(dto.C2CMessageCreate, OnCommand("test"))
			results <- m
		}()
	}

	// 收集结果
	successCount := 0
	failCount := 0
	for i := 0; i < 100; i++ {
		m := <-results
		if m != noopMatcher {
			successCount++
		} else {
			failCount++
		}
	}

	// 应该有正好 50 个成功
	if successCount != 50 {
		t.Errorf("Expected 50 successful registrations, got %d", successCount)
	}

	// 验证最终数量
	if engine.GetMatcherCount() != 50 {
		t.Errorf("Expected 50 matchers, got %d", engine.GetMatcherCount())
	}
}
