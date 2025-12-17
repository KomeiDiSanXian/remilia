package remilia

import (
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestRegexCacheLRU 测试正则缓存 LRU 功能
func TestRegexCacheLRU(t *testing.T) {
	// 清空并设置小容量
	ClearRegexCache()
	SetRegexCacheMaxSize(3)

	// 添加 3 个正则
	OnRegex("pattern1")
	OnRegex("pattern2")
	OnRegex("pattern3")

	assert.Equal(t, 3, GetRegexCacheSize(), "should have 3 patterns cached")

	// 添加第 4 个，应该淘汰最旧的
	OnRegex("pattern4")

	assert.Equal(t, 3, GetRegexCacheSize(), "should still have 3 patterns after eviction")

	// 恢复默认大小
	SetRegexCacheMaxSize(1000)
}

// TestRegexCacheEviction 测试缓存淘汰策略
func TestRegexCacheEviction(t *testing.T) {
	ClearRegexCache()
	SetRegexCacheMaxSize(5)

	// 添加 5 个模式
	patterns := []string{"p1", "p2", "p3", "p4", "p5"}
	for _, p := range patterns {
		OnRegex(p)
	}

	assert.Equal(t, 5, GetRegexCacheSize())

	// 访问 p1，使其成为最近使用
	rule, _ := OnRegexSafe("p1")
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	ctx.SetState("message_content", "test")
	rule(ctx)

	// 添加新模式，应该淘汰未访问的
	OnRegex("p6")

	assert.Equal(t, 5, GetRegexCacheSize(), "size should not exceed limit")

	SetRegexCacheMaxSize(1000)
}

// TestRegexCacheThreadSafe 测试缓存并发安全
func TestRegexCacheThreadSafe(t *testing.T) {
	ClearRegexCache()

	done := make(chan bool)

	// 启动多个 goroutine 并发访问缓存
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 100; j++ {
				pattern := fmt.Sprintf("pattern-%d-%d", id, j)
				OnRegex(pattern)
			}
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 验证缓存没有问题
	size := GetRegexCacheSize()
	assert.Greater(t, size, 0, "cache should contain patterns")
	assert.LessOrEqual(t, size, 1000, "cache should not exceed max size")
}

// TestSetRegexCacheMaxSize 测试设置最大容量
func TestSetRegexCacheMaxSize(t *testing.T) {
	ClearRegexCache()

	// 设置大容量
	SetRegexCacheMaxSize(2000)
	assert.Equal(t, 2000, GetRegexCacheMaxSize())

	// 设置小容量
	SetRegexCacheMaxSize(500)
	assert.Equal(t, 500, GetRegexCacheMaxSize())

	// 设置零或负值应该使用默认值
	SetRegexCacheMaxSize(0)
	assert.Equal(t, 1000, GetRegexCacheMaxSize())

	SetRegexCacheMaxSize(-100)
	assert.Equal(t, 1000, GetRegexCacheMaxSize())
}

// TestSetRegexCacheMaxSizeWithEviction 测试缩小容量时淘汰
func TestSetRegexCacheMaxSizeWithEviction(t *testing.T) {
	ClearRegexCache()
	SetRegexCacheMaxSize(100)

	// 添加 50 个模式
	for i := 0; i < 50; i++ {
		OnRegex(fmt.Sprintf("pattern-%d", i))
	}

	assert.Equal(t, 50, GetRegexCacheSize())

	// 缩小容量到 20
	SetRegexCacheMaxSize(20)

	// 验证已淘汰多余的条目
	assert.Equal(t, 20, GetRegexCacheSize(), "should evict excess entries")

	SetRegexCacheMaxSize(1000)
}

// TestClearRegexCache 测试清空缓存
func TestClearRegexCache(t *testing.T) {
	// 添加一些模式
	OnRegex("test1")
	OnRegex("test2")
	OnRegex("test3")

	initialSize := GetRegexCacheSize()
	assert.Greater(t, initialSize, 0)

	// 清空缓存
	ClearRegexCache()

	assert.Equal(t, 0, GetRegexCacheSize(), "cache should be empty after clear")
}

// TestRegexCacheHitRate 测试缓存命中率
func TestRegexCacheHitRate(t *testing.T) {
	ClearRegexCache()

	pattern := "test-pattern-\\d+"

	// 第一次调用会编译并缓存
	rule1 := OnRegex(pattern)

	size1 := GetRegexCacheSize()

	// 第二次调用应该从缓存获取
	rule2 := OnRegex(pattern)

	size2 := GetRegexCacheSize()

	// 验证缓存没有增加（说明使用了缓存）
	assert.Equal(t, size1, size2, "cache size should not change for same pattern")

	// 验证规则功能正常
	assert.NotNil(t, rule1, "first rule should not be nil")
	assert.NotNil(t, rule2, "second rule should not be nil")
}

// TestRegexCacheSafeVersion 测试 OnRegexSafe 的缓存
func TestRegexCacheSafeVersion(t *testing.T) {
	ClearRegexCache()

	pattern := "valid-pattern-\\d+"

	// 第一次调用
	rule1, err1 := OnRegexSafe(pattern)
	assert.NoError(t, err1)
	assert.NotNil(t, rule1)

	size1 := GetRegexCacheSize()

	// 第二次调用（应该从缓存获取）
	rule2, err2 := OnRegexSafe(pattern)
	assert.NoError(t, err2)
	assert.NotNil(t, rule2)

	size2 := GetRegexCacheSize()

	// 缓存大小不应该改变（说明使用了缓存）
	assert.Equal(t, size1, size2, "cache size should not change for same pattern")
}

// TestRegexCacheInvalidPattern 测试无效正则不会缓存
func TestRegexCacheInvalidPattern(t *testing.T) {
	ClearRegexCache()

	initialSize := GetRegexCacheSize()

	// 尝试使用无效正则
	_, err := OnRegexSafe("[invalid")
	assert.Error(t, err, "should return error for invalid pattern")

	// 验证缓存大小没有增加
	assert.Equal(t, initialSize, GetRegexCacheSize(), "invalid pattern should not be cached")
}

// TestRegexCacheAccessTimeUpdate 测试访问时间更新
func TestRegexCacheAccessTimeUpdate(t *testing.T) {
	ClearRegexCache()
	SetRegexCacheMaxSize(3)

	// 添加 3 个模式
	OnRegex("p1")
	time.Sleep(10 * time.Millisecond)
	OnRegex("p2")
	time.Sleep(10 * time.Millisecond)
	OnRegex("p3")

	// 访问 p1（更新访问时间）
	OnRegex("p1")

	// 添加 p4，应该淘汰 p2（p1 和 p3 访问时间更新了）
	OnRegex("p4")

	// 尝试访问 p2，应该需要重新编译（被淘汰了）
	sizeBefore := GetRegexCacheSize()
	OnRegex("p2")
	sizeAfter := GetRegexCacheSize()

	// 如果 p2 被淘汰，重新添加时缓存大小应该保持不变（因为同时淘汰了其他的）
	assert.Equal(t, 3, sizeBefore)
	assert.Equal(t, 3, sizeAfter)

	SetRegexCacheMaxSize(1000)
}

// TestRegexCacheDifferentPatterns 测试不同模式独立缓存
func TestRegexCacheDifferentPatterns(t *testing.T) {
	ClearRegexCache()

	patterns := []string{
		"pattern1",
		"pattern2",
		"[a-z]+",
		"\\d{3,5}",
		"^start.*end$",
	}

	// 添加所有模式
	for _, p := range patterns {
		OnRegex(p)
	}

	assert.Equal(t, len(patterns), GetRegexCacheSize(), "each pattern should be cached separately")
}

// BenchmarkRegexCacheHit 基准测试缓存命中性能
func BenchmarkRegexCacheHit(b *testing.B) {
	ClearRegexCache()
	pattern := "test-pattern-\\d+"

	// 预热缓存
	OnRegex(pattern)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OnRegex(pattern)
	}
}

// BenchmarkRegexCacheMiss 基准测试缓存未命中性能
func BenchmarkRegexCacheMiss(b *testing.B) {
	ClearRegexCache()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern := fmt.Sprintf("pattern-%d", i)
		OnRegex(pattern)
	}
}

// BenchmarkRegexCacheEviction 基准测试缓存淘汰性能
func BenchmarkRegexCacheEviction(b *testing.B) {
	ClearRegexCache()
	SetRegexCacheMaxSize(100)

	// 预填充缓存
	for i := 0; i < 100; i++ {
		OnRegex(fmt.Sprintf("pattern-%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern := fmt.Sprintf("new-pattern-%d", i)
		OnRegex(pattern)
	}

	SetRegexCacheMaxSize(1000)
}

// BenchmarkRegexCacheConcurrent 基准测试并发访问性能
func BenchmarkRegexCacheConcurrent(b *testing.B) {
	ClearRegexCache()
	patterns := make([]string, 100)
	for i := 0; i < 100; i++ {
		patterns[i] = fmt.Sprintf("pattern-%d", i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			OnRegex(patterns[i%len(patterns)])
			i++
		}
	})
}
