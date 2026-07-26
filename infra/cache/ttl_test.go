package cache_test

import (
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 基础功能 ──────────────────────────────────────────────────────────────────

func TestMap_SetAndGet(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("a", 1, time.Minute)
	v, ok := m.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestMap_GetMissing(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	v, ok := m.Get("nonexistent")
	assert.False(t, ok)
	assert.Zero(t, v)
}

func TestMap_Overwrite(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("k", 1, time.Minute)
	m.Set("k", 2, time.Minute)
	v, ok := m.Get("k")
	assert.True(t, ok)
	assert.Equal(t, 2, v)
}

// ─── TTL 过期 ──────────────────────────────────────────────────────────────────

func TestMap_Expiration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](0)
		defer m.Stop()

		m.Set("x", 42, 50*time.Millisecond)

		// 未过期时可取到
		v, ok := m.Get("x")
		require.True(t, ok)
		assert.Equal(t, 42, v)

		// 等待过期（synctest 中使用虚拟时间，瞬间完成）
		time.Sleep(100 * time.Millisecond)
		_, ok = m.Get("x")
		assert.False(t, ok, "entry should be expired")
	})
}

func TestMap_ZeroTTL_ExpiresImmediately(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("k", 99, 0)
	// TTL=0 意味着立即过期
	_, ok := m.Get("k")
	assert.False(t, ok)
}

// ─── GetWithTTL ────────────────────────────────────────────────────────────────

func TestMap_GetWithTTL(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("x", 7, time.Minute)
	v, rem, ok := m.GetWithTTL("x")
	assert.True(t, ok)
	assert.Equal(t, 7, v)
	assert.Greater(t, rem, time.Duration(0))
	assert.LessOrEqual(t, rem, time.Minute)
}

func TestMap_GetWithTTL_Missing(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	_, rem, ok := m.GetWithTTL("missing")
	assert.False(t, ok)
	assert.Zero(t, rem)
}

func TestMap_GetWithTTL_Expired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](0)
		defer m.Stop()

		m.Set("x", 1, 30*time.Millisecond)
		time.Sleep(60 * time.Millisecond)
		_, rem, ok := m.GetWithTTL("x")
		assert.False(t, ok)
		assert.Zero(t, rem)
	})
}

// ─── Has ───────────────────────────────────────────────────────────────────────

func TestMap_Has(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("a", 1, time.Minute)
	assert.True(t, m.Has("a"))
	assert.False(t, m.Has("b"))
}

// ─── Delete ────────────────────────────────────────────────────────────────────

func TestMap_Delete(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("a", 1, time.Minute)
	m.Delete("a")
	assert.False(t, m.Has("a"))
}

func TestMap_DeleteNonExistent(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()
	// 不应 panic
	m.Delete("ghost")
}

// ─── Len / Cap ─────────────────────────────────────────────────────────────────

func TestMap_Len(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](0)
		defer m.Stop()

		m.Set("a", 1, 50*time.Millisecond)
		m.Set("b", 2, time.Minute)
		assert.Equal(t, 2, m.Len())

		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, 1, m.Len())
	})
}

func TestMap_Cap_IncludesExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](0)
		defer m.Stop()

		m.Set("a", 1, 30*time.Millisecond)
		m.Set("b", 2, time.Minute)
		assert.Equal(t, 2, m.Cap())

		time.Sleep(60 * time.Millisecond)
		assert.Equal(t, 2, m.Cap())
		m.GC()
		assert.Equal(t, 1, m.Cap())
	})
}

// ─── GC ────────────────────────────────────────────────────────────────────────

func TestMap_GC_RemovesExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](0)
		defer m.Stop()

		m.Set("a", 1, 30*time.Millisecond)
		m.Set("b", 2, time.Minute)

		time.Sleep(60 * time.Millisecond)
		removed := m.GC()
		assert.Equal(t, 1, removed)
		assert.Equal(t, 1, m.Cap())
		assert.True(t, m.Has("b"))
	})
}

func TestMap_GC_NothingToRemove(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("a", 1, time.Minute)
	assert.Equal(t, 0, m.GC())
}

// ─── Flush ─────────────────────────────────────────────────────────────────────

func TestMap_Flush(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("a", 1, time.Minute)
	m.Set("b", 2, time.Minute)
	m.Flush()
	assert.Equal(t, 0, m.Len())
	assert.Equal(t, 0, m.Cap())
}

// ─── Keys ──────────────────────────────────────────────────────────────────────

func TestMap_Keys(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("a", 1, time.Minute)
	m.Set("b", 2, time.Minute)
	keys := m.Keys()
	assert.ElementsMatch(t, []string{"a", "b"}, keys)
}

func TestMap_Keys_ExcludesExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](0)
		defer m.Stop()

		m.Set("a", 1, 30*time.Millisecond)
		m.Set("b", 2, time.Minute)

		time.Sleep(60 * time.Millisecond)
		keys := m.Keys()
		assert.Equal(t, []string{"b"}, keys)
	})
}

// ─── Range ─────────────────────────────────────────────────────────────────────

func TestMap_Range(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	m.Set("a", 1, time.Minute)
	m.Set("b", 2, time.Minute)

	seen := map[string]int{}
	m.Range(func(k string, v int, _ time.Duration) bool {
		seen[k] = v
		return true
	})
	assert.Equal(t, map[string]int{"a": 1, "b": 2}, seen)
}

func TestMap_Range_EarlyStop(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	for i := range 10 {
		m.Set(fmt.Sprintf("k%d", i), i, time.Minute)
	}

	count := 0
	m.Range(func(_ string, _ int, _ time.Duration) bool {
		count++
		return count < 3 // 只迭代 3 个后停止
	})
	assert.Equal(t, 3, count)
}

// ─── 后台 GC ───────────────────────────────────────────────────────────────────

func TestMap_BackgroundGC(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](40 * time.Millisecond)
		defer m.Stop()

		m.Set("a", 1, 20*time.Millisecond)
		m.Set("b", 2, time.Minute)

		time.Sleep(200 * time.Millisecond)

		assert.Equal(t, 1, m.Cap(), "background GC should have removed expired entry")
		assert.True(t, m.Has("b"))
	})
}

// ─── H-4 修复验证：GC 与 Get 的过期语义一致性 ─────────────────────────────────

// TestMap_GC_DeadlineEqualsNow 验证 H-4 修复：
// GC 使用 !now.Before(deadline)（>= 语义），与 Get 保持一致。
// 修复前 GC 使用 now.After(deadline)（严格 >），导致 deadline==now 时
// Get 返回 false（已过期）但 GC 不删除，造成内存残留。
func TestMap_GC_DeadlineEqualsNow(t *testing.T) {
	m := cache.New[string, int](0)
	defer m.Stop()

	// TTL=0 → deadline = time.Now().Add(0)，set 之后立即过期
	m.Set("boundary", 42, 0)

	// Get 应将 deadline<=now 视为过期（>= 语义）
	_, ok := m.Get("boundary")
	assert.False(t, ok, "Get: TTL=0 条目（deadline==now）应被视为已过期")

	// GC 也应将同样条目删除（修复前 now.After(deadline) 严格大于，相等时不删）
	removed := m.GC()
	assert.Equal(t, 1, removed, "GC: TTL=0 条目应被 GC 删除（H-4 fix：>= 语义与 Get 一致）")
	assert.Equal(t, 0, m.Cap(), "GC 后内部 map 应不含任何残留条目")
}

// TestMap_GC_SemanticMatchesGet 验证 GC 与 Get 对同一批条目的过期判断完全一致。
// 向 map 写入多条不同 TTL 的条目，等待部分过期后，
// 对比 Get 认为已过期的集合与 GC 实际删除的集合是否相等。
func TestMap_GC_SemanticMatchesGet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := cache.New[string, int](0)
		defer m.Stop()

		m.Set("expired-1", 1, 1*time.Nanosecond)
		m.Set("expired-2", 2, 1*time.Nanosecond)
		m.Set("alive", 3, time.Hour)

		// 等待 1ms 确保 expired-* 条目已过期
		time.Sleep(time.Millisecond)

		expiredByGet := 0
		for _, k := range []string{"expired-1", "expired-2", "alive"} {
			_, ok := m.Get(k)
			if !ok {
				expiredByGet++
			}
		}
		assert.Equal(t, 2, expiredByGet, "Get 应认为 2 个短 TTL 条目已过期")

		removed := m.GC()
		assert.Equal(t, expiredByGet, removed,
			"GC 删除数应与 Get 过期数相同（语义一致性验证）")
		assert.Equal(t, 1, m.Cap(), "GC 后只剩 'alive' 条目")
		assert.True(t, m.Has("alive"), "'alive' 条目不应被 GC 删除")
	})
}

// ─── Stop 幂等 ─────────────────────────────────────────────────────────────────

func TestMap_StopIdempotent(t *testing.T) {
	m := cache.New[string, int](time.Minute)
	// 多次调用 Stop 不应 panic
	m.Stop()
	m.Stop()
	m.Stop()
}

// ─── 并发安全 ──────────────────────────────────────────────────────────────────

func TestMap_ConcurrentAccess(t *testing.T) {
	m := cache.New[string, int](10 * time.Millisecond)
	defer m.Stop()

	const goroutines = 50
	const iters = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for i := range iters {
				key := fmt.Sprintf("g%d-k%d", id, i%10)
				m.Set(key, i, 50*time.Millisecond)
				m.Get(key)
				if i%20 == 0 {
					m.GC()
				}
			}
		}(g)
	}
	wg.Wait()
}

// ─── 泛型类型测试 ──────────────────────────────────────────────────────────────

func TestMap_StructValue(t *testing.T) {
	type Item struct{ Name string }
	m := cache.New[int, Item](0)
	defer m.Stop()

	m.Set(1, Item{Name: "hello"}, time.Minute)
	v, ok := m.Get(1)
	require.True(t, ok)
	assert.Equal(t, "hello", v.Name)
}

func TestMap_PointerValue(t *testing.T) {
	type Session struct{ Token string }
	m := cache.New[string, *Session](0)
	defer m.Stop()

	m.Set("sess-1", &Session{Token: "abc"}, time.Minute)
	v, ok := m.Get("sess-1")
	require.True(t, ok)
	assert.Equal(t, "abc", v.Token)
}
