package remilia

import (
	"regexp"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestRegexCache_BasicCaching 测试正则表达式缓存基本功能
func TestRegexCache_BasicCaching(t *testing.T) {
	// 清空缓存
	ClearRegexCache()
	assert.Equal(t, 0, GetRegexCacheSize())

	// 第一次调用，应该编译并缓存
	rule1 := OnRegex(`^\d+$`)
	assert.Equal(t, 1, GetRegexCacheSize())

	// 相同模式再次调用，应该从缓存获取
	rule2 := OnRegex(`^\d+$`)
	assert.Equal(t, 1, GetRegexCacheSize(), "Same pattern should use cache")

	// 不同模式，应该新增缓存
	rule3 := OnRegex(`^[a-z]+$`)
	assert.Equal(t, 2, GetRegexCacheSize())

	// 测试规则功能正常
	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "123"}`),
	}, nil)

	assert.True(t, rule1(ctx), "rule1 should match digits")
	assert.True(t, rule2(ctx), "rule2 should match digits (same as rule1)")
	assert.False(t, rule3(ctx), "rule3 should not match digits")

	// 创建新的 context 测试字母
	ctx2 := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "abc"}`),
	}, nil)
	assert.False(t, rule1(ctx2), "rule1 should not match letters")
	assert.True(t, rule3(ctx2), "rule3 should match letters")
}

// TestRegexCache_Safe 测试 OnRegexSafe 也使用缓存
func TestRegexCache_Safe(t *testing.T) {
	ClearRegexCache()

	// 有效的正则表达式
	rule1, err1 := OnRegexSafe(`^test$`)
	assert.NoError(t, err1)
	assert.Equal(t, 1, GetRegexCacheSize())

	// 相同模式应该从缓存获取
	rule2, err2 := OnRegexSafe(`^test$`)
	assert.NoError(t, err2)
	assert.Equal(t, 1, GetRegexCacheSize(), "Should use cached regex")

	// 无效的正则表达式
	_, err3 := OnRegexSafe(`[invalid(`)
	assert.Error(t, err3, "Invalid regex should return error")
	assert.Equal(t, 1, GetRegexCacheSize(), "Invalid regex should not be cached")

	// 测试功能
	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "test"}`),
	}, nil)

	assert.True(t, rule1(ctx))
	assert.True(t, rule2(ctx))
}

// TestRegexCache_Performance 测试缓存性能提升
func TestRegexCache_Performance(t *testing.T) {
	ClearRegexCache()

	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`

	// 重复调用相同模式
	for i := 0; i < 100; i++ {
		OnRegex(pattern)
	}

	// 应该只有一个缓存项
	assert.Equal(t, 1, GetRegexCacheSize(), "100 calls should result in 1 cached item")
}

// TestRegexCache_Clear 测试缓存清理
func TestRegexCache_Clear(t *testing.T) {
	ClearRegexCache()

	OnRegex(`pattern1`)
	OnRegex(`pattern2`)
	OnRegex(`pattern3`)
	assert.Equal(t, 3, GetRegexCacheSize())

	ClearRegexCache()
	assert.Equal(t, 0, GetRegexCacheSize(), "Cache should be empty after clear")

	// 清空后再次调用应该重新编译
	OnRegex(`pattern1`)
	assert.Equal(t, 1, GetRegexCacheSize())
}

// TestRegexCache_Concurrent 测试并发场景下的缓存安全性
func TestRegexCache_Concurrent(t *testing.T) {
	ClearRegexCache()

	pattern := `^\d{3}-\d{4}$`
	done := make(chan bool)

	// 并发调用相同模式
	for i := 0; i < 10; i++ {
		go func() {
			OnRegex(pattern)
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 应该只有一个缓存项（sync.Map 保证并发安全）
	assert.Equal(t, 1, GetRegexCacheSize(), "Concurrent calls should result in 1 cached item")
}

// BenchmarkRegex_WithCache 测试带缓存的正则性能
func BenchmarkRegex_WithCache(b *testing.B) {
	ClearRegexCache()
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`

	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "test@example.com"}`),
	}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rule := OnRegex(pattern)
		_ = rule(ctx)
	}
}

// BenchmarkRegex_WithoutCache 测试不带缓存的正则性能（模拟）
func BenchmarkRegex_WithoutCache(b *testing.B) {
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`

	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "test@example.com"}`),
	}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 直接编译（不使用缓存）
		rule := OnRegexCompiled(regexp.MustCompile(pattern))
		_ = rule(ctx)
	}
}
