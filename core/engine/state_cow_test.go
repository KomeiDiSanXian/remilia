package engine

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
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
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}

	// 复制状态
	dst := src.clone()

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
	src := newEngineState()
	m1 := &Matcher{
		EventType: string(platform.EventKindPrivateMessage),
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test1",
	}
	m1.priority.Store(10)
	src.addMatcher(m1)

	dst := src.clone()

	m2 := &Matcher{
		EventType: string(platform.EventKindPrivateMessage),
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test2",
	}
	m2.priority.Store(20)
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

	m1 := &Matcher{
		EventType: string(platform.EventKindPrivateMessage),
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test1",
	}
	m1.priority.Store(10)
	state.addMatcher(m1)

	oldCap := cap(state.sortedCache[string(platform.EventKindPrivateMessage)])

	m2 := &Matcher{
		EventType: string(platform.EventKindPrivateMessage),
		Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
		Handler:   func(ctx *context.Context) error { return nil },
		Source:    "test2",
	}
	m2.priority.Store(20)
	state.addMatcher(m2)

	// 如果容量足够，应该重用
	newCap := cap(state.sortedCache[string(platform.EventKindPrivateMessage)])
	if oldCap >= 2 && newCap != oldCap {
		t.Logf("Capacity changed from %d to %d (reuse optimization may not have triggered)", oldCap, newCap)
	}

	// 验证功能正确性
	if len(state.sortedCache[string(platform.EventKindPrivateMessage)]) != 2 {
		t.Errorf("sortedCache should have 2 matchers, got %d", len(state.sortedCache[string(platform.EventKindPrivateMessage)]))
	}
}

// TestInvalidateSortedCacheOptimization 测试缓存失效优化
func TestInvalidateSortedCacheOptimization(t *testing.T) {
	state := newEngineState()

	// 添加多个 matchers
	for i := range 5 {
		m := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		m.priority.Store(uint64(i * 10))
		state.addMatcher(m)
	}

	// 记录容量
	oldCap := cap(state.sortedCache[string(platform.EventKindPrivateMessage)])

	// 失效缓存
	state.invalidateSortedCache(string(platform.EventKindPrivateMessage))

	// 验证容量重用
	newCap := cap(state.sortedCache[string(platform.EventKindPrivateMessage)])
	if newCap != oldCap {
		t.Logf("Capacity changed from %d to %d (may allocate new if needed)", oldCap, newCap)
	}

	// 验证功能正确性
	if len(state.sortedCache[string(platform.EventKindPrivateMessage)]) != 5 {
		t.Errorf("sortedCache should still have 5 matchers, got %d", len(state.sortedCache[string(platform.EventKindPrivateMessage)]))
	}

	// 验证排序正确性
	sorted := state.sortedCache[string(platform.EventKindPrivateMessage)]
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
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.clone()
	}
}

// BenchmarkWithBlock 基准测试：SetBlock 性能（O(1)，不复制任何 map）
func BenchmarkWithBlock(b *testing.B) {
	src := newEngineState()
	for i := range 10000 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withBlock(true)
	}
}

// BenchmarkWithMaxMatchers 基准测试：SetMaxMatchers 性能（O(1)，不复制任何 map）
func BenchmarkWithMaxMatchers(b *testing.B) {
	src := newEngineState()
	for i := range 10000 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withMaxMatchers(500)
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
			chain: make([]context.Middleware, 10),
			gen:   uint64(i),
		}
		src.groupMiddlewares[groupName] = snap
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = copyMiddlewareState(src)
	}
}

// BenchmarkAddMatcher 基准测试：添加 matcher 性能（旧方式：clone + mutate）
func BenchmarkAddMatcherOld(b *testing.B) {
	src := newEngineState()
	for i := range 50 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	newMatcher := &Matcher{EventType: "evt", Source: "bench"}
	newMatcher.priority.Store(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := src.clone()
		dst.addMatcher(newMatcher)
	}
}

// BenchmarkWithAddedMatcher 基准测试：添加 matcher 性能（新方式：withAddedMatcher）
func BenchmarkWithAddedMatcher(b *testing.B) {
	src := newEngineState()
	for i := range 50 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	newMatcher := &Matcher{EventType: "evt", Source: "bench"}
	newMatcher.priority.Store(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withAddedMatcher(newMatcher)
	}
}

// BenchmarkWithAddedMatcherLarge 基准测试：大状态（10000 matchers）+ 添加 matcher
func BenchmarkWithAddedMatcherLarge(b *testing.B) {
	src := newEngineState()
	for i := range 10000 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	newMatcher := &Matcher{EventType: "evt", Source: "bench"}
	newMatcher.priority.Store(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withAddedMatcher(newMatcher)
	}
}

// BenchmarkWithAddedCommandMatcher 基准测试：添加 command matcher（需要复制 commandIndex）
func BenchmarkWithAddedCommandMatcher(b *testing.B) {
	src := newEngineState()
	for i := range 50 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	for i := range 20 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.definition = &command.Definition{Name: "existing"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	newCmdMatcher := &Matcher{EventType: "evt2", Source: "bench"}
	newCmdMatcher.definition = &command.Definition{Name: "newcmd"}
	newCmdMatcher.priority.Store(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withAddedMatcher(newCmdMatcher)
	}
}

// BenchmarkInvalidateSortedCache 基准测试：缓存失效性能
func BenchmarkInvalidateSortedCache(b *testing.B) {
	state := newEngineState()

	for i := range 100 {
		m := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		m.priority.Store(uint64(i))
		state.addMatcher(m)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.invalidateSortedCache(string(platform.EventKindPrivateMessage))
	}
}

// BenchmarkCOWModificationOld 基准测试：COW 修改性能（旧方式：clone + mutate）
func BenchmarkCOWModificationOld(b *testing.B) {
	src := newEngineState()
	for i := range 50 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	newMatcher := &Matcher{EventType: "evt", Source: "bench"}
	newMatcher.priority.Store(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := src.clone()
		dst.addMatcher(newMatcher)
	}
}

// BenchmarkCOWModificationNew 基准测试：COW 修改性能（新方式：withAddedMatcher）
func BenchmarkCOWModificationNew(b *testing.B) {
	src := newEngineState()
	for i := range 50 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	newMatcher := &Matcher{EventType: "evt", Source: "bench"}
	newMatcher.priority.Store(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withAddedMatcher(newMatcher)
	}
}

// BenchmarkWithSetBlockLarge 基准测试：大状态 SetBlock（旧方式需要全量 copy，新方式 O(1)）
func BenchmarkWithSetBlockLarge(b *testing.B) {
	src := newEngineState()
	for i := range 10000 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withBlock(true)
	}
}

// BenchmarkWithInvalidatedSortedCache 基准测试：sortedCache 失效
func BenchmarkWithInvalidatedSortedCache(b *testing.B) {
	src := newEngineState()
	for i := range 1000 {
		m := &Matcher{EventType: "evt", Source: "bench"}
		m.priority.Store(uint64(i))
		src.addMatcher(m)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = src.withInvalidatedSortedCache("evt")
	}
}
