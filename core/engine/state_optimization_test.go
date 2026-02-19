package engine

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestCopyEngineState 测试状态复制的正确性
func TestCopyEngineState(t *testing.T) {
	// 创建原始状态
	src := newEngineState()
	src.maxMatchers = 100
	src.block = true

	// 添加一些 matchers
	for i := range 5 {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
			priority:  uint(i),
		}
		src.addMatcher(m)
	}

	// 复制状态
	dst := copyEngineState(src)

	// 验证复制正确性
	if dst.maxMatchers != src.maxMatchers {
		t.Errorf("maxMatchers not copied correctly: got %d, want %d", dst.maxMatchers, src.maxMatchers)
	}

	if dst.block != src.block {
		t.Errorf("block not copied correctly: got %v, want %v", dst.block, src.block)
	}

	if len(dst.matchers) != len(src.matchers) {
		t.Errorf("matchers length mismatch: got %d, want %d", len(dst.matchers), len(src.matchers))
	}

	// 验证索引复制
	if len(dst.matcherIndex) != len(src.matcherIndex) {
		t.Errorf("matcherIndex length mismatch: got %d, want %d", len(dst.matcherIndex), len(src.matcherIndex))
	}

	if len(dst.sortedCache) != len(src.sortedCache) {
		t.Errorf("sortedCache length mismatch: got %d, want %d", len(dst.sortedCache), len(src.sortedCache))
	}
}

// TestCopyEngineStateCOW 测试 COW 行为
func TestCopyEngineStateCOW(t *testing.T) {
	// 创建原始状态
	src := newEngineState()
	m1 := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test1",
		priority:  10,
	}
	src.addMatcher(m1)

	// 复制状态
	dst := copyEngineState(src)

	// 修改目标状态
	m2 := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test2",
		priority:  20,
	}
	dst.addMatcher(m2)

	// 验证原始状态未被修改
	if len(src.matchers) != 1 {
		t.Errorf("source matchers should not be modified: got %d, want 1", len(src.matchers))
	}

	if len(dst.matchers) != 2 {
		t.Errorf("destination matchers should have 2: got %d, want 2", len(dst.matchers))
	}
}

// TestCopyMiddlewareState 测试中间件状态复制
func TestCopyMiddlewareState(t *testing.T) {
	src := newMiddlewareState()

	// 添加全局中间件
	mw1 := func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			return next(ctx)
		}
	}
	src.global.chain = append(src.global.chain, mw1)
	src.global.gen = 5

	// 复制
	dst := copyMiddlewareState(src)

	// 验证
	if len(dst.global.chain) != len(src.global.chain) {
		t.Errorf("global chain length mismatch: got %d, want %d", len(dst.global.chain), len(src.global.chain))
	}

	if dst.global.gen != src.global.gen {
		t.Errorf("global gen mismatch: got %d, want %d", dst.global.gen, src.global.gen)
	}
}

// TestAddMatcherOptimization 测试添加 matcher 的优化
func TestAddMatcherOptimization(t *testing.T) {
	state := newEngineState()

	// 添加第一个 matcher
	m1 := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test1",
		priority:  10,
	}
	state.addMatcher(m1)

	// 记录 sortedCache 的容量
	oldCap := cap(state.sortedCache[dto.C2CMessageCreate])

	// 添加第二个 matcher
	m2 := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test2",
		priority:  20,
	}
	state.addMatcher(m2)

	// 如果容量足够，应该重用
	newCap := cap(state.sortedCache[dto.C2CMessageCreate])
	if oldCap >= 2 && newCap != oldCap {
		t.Logf("Capacity changed from %d to %d (reuse optimization may not have triggered)", oldCap, newCap)
	}

	// 验证功能正确性
	if len(state.sortedCache[dto.C2CMessageCreate]) != 2 {
		t.Errorf("sortedCache should have 2 matchers, got %d", len(state.sortedCache[dto.C2CMessageCreate]))
	}
}

// TestInvalidateSortedCacheOptimization 测试缓存失效优化
func TestInvalidateSortedCacheOptimization(t *testing.T) {
	state := newEngineState()

	// 添加多个 matchers
	for i := range 5 {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
			priority:  uint(i * 10),
		}
		state.addMatcher(m)
	}

	// 记录容量
	oldCap := cap(state.sortedCache[dto.C2CMessageCreate])

	// 失效缓存
	state.invalidateSortedCache(dto.C2CMessageCreate)

	// 验证容量重用
	newCap := cap(state.sortedCache[dto.C2CMessageCreate])
	if newCap != oldCap {
		t.Logf("Capacity changed from %d to %d (may allocate new if needed)", oldCap, newCap)
	}

	// 验证功能正确性
	if len(state.sortedCache[dto.C2CMessageCreate]) != 5 {
		t.Errorf("sortedCache should still have 5 matchers, got %d", len(state.sortedCache[dto.C2CMessageCreate]))
	}

	// 验证排序正确性
	sorted := state.sortedCache[dto.C2CMessageCreate]
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1].getPriority() > sorted[i].getPriority() {
			t.Errorf("sortedCache not properly sorted at index %d", i)
		}
	}
}

// BenchmarkCopyEngineState 基准测试：状态复制性能
func BenchmarkCopyEngineState(b *testing.B) {
	// 创建包含 100 个 matchers 的状态
	src := newEngineState()
	for i := range 100 {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
			priority:  uint(i),
		}
		src.addMatcher(m)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = copyEngineState(src)
	}
}

// BenchmarkCopyMiddlewareState 基准测试：中间件状态复制性能
func BenchmarkCopyMiddlewareState(b *testing.B) {
	src := newMiddlewareState()

	// 添加 10 个全局中间件
	for range 10 {
		mw := func(next context.Handler) context.Handler {
			return func(ctx *context.Context) error {
				return next(ctx)
			}
		}
		src.global.chain = append(src.global.chain, mw)
	}

	// 添加 5 个分组中间件
	for i := range 5 {
		groupName := "group" + string(rune('A'+i))
		snap := &middlewareSnapshot{
			chain: make([]Middleware, 10),
			gen:   uint64(i),
		}
		src.groupMiddlewares[groupName] = snap
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = copyMiddlewareState(src)
	}
}

// BenchmarkAddMatcher 基准测试：添加 matcher 性能
func BenchmarkAddMatcher(b *testing.B) {
	state := newEngineState()

	// 预分配一些 matchers 以模拟真实场景
	for i := range 50 {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
			priority:  uint(i),
		}
		state.addMatcher(m)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "bench",
			priority:  100,
		}
		// 创建新状态来测试
		stateCopy := copyEngineState(state)
		stateCopy.addMatcher(m)
	}
}

// BenchmarkInvalidateSortedCache 基准测试：缓存失效性能
func BenchmarkInvalidateSortedCache(b *testing.B) {
	state := newEngineState()

	// 添加 100 个 matchers
	for i := range 100 {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
			priority:  uint(i),
		}
		state.addMatcher(m)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.invalidateSortedCache(dto.C2CMessageCreate)
	}
}

// BenchmarkCOWModification 基准测试：COW 修改性能
func BenchmarkCOWModification(b *testing.B) {
	// 创建初始状态
	src := newEngineState()
	for i := range 50 {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
			priority:  uint(i),
		}
		src.addMatcher(m)
	}

	newMatcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "new",
		priority:  100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// COW 流程：复制 -> 修改
		dst := copyEngineState(src)
		dst.addMatcher(newMatcher)
	}
}
