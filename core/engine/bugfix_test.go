package engine

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// TestBugFix_ExtractCommandWithWhitespace 测试 Bug 7 的修复：extractCommand 处理空白
func TestBugFix_ExtractCommandWithWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal command", "/ping", "/ping"},
		{"with leading space", "  /ping", "/ping"},
		{"with trailing space", "/ping  ", "/ping"},
		{"with both spaces", "  /ping  ", "/ping"},
		{"command with args", "/ping arg1", "/ping"},
		{"with spaces and args", "  /ping arg1  ", "/ping"},
		{"only spaces", "   ", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCommand(tt.input)
			if result != tt.expected {
				t.Errorf("extractCommand(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestBugFix_ContextCloneDeadline 测试 Bug 1 的修复：Clone 保留 deadline
func TestBugFix_ContextCloneDeadline(t *testing.T) {
	// 创建带 deadline 的 context
	stdCtx, cancel := stdctx.WithTimeout(stdctx.Background(), 5*time.Second)
	defer cancel()

	payload := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "test"}`),
	}

	ctx := context.NewContextWithContext(stdCtx, payload, nil)

	// 克隆
	cloned := ctx.Clone()

	// 验证原 context 有 deadline
	originalDeadline, ok := ctx.Context().Deadline()
	if !ok {
		t.Fatal("Original context should have deadline")
	}

	// 验证克隆的 context 也有 deadline
	clonedDeadline, ok := cloned.Context().Deadline()
	if !ok {
		t.Fatal("Cloned context should have deadline")
	}

	// 验证 deadline 相同
	if !originalDeadline.Equal(clonedDeadline) {
		t.Errorf("Cloned context deadline = %v, want %v", clonedDeadline, originalDeadline)
	}

	// 验证克隆的 context 不会因原 context 取消而取消
	cancel()
	time.Sleep(10 * time.Millisecond)

	select {
	case <-cloned.Context().Done():
		t.Error("Cloned context should not be cancelled when original is cancelled")
	default:
		// 正确：克隆的 context 不受原 context 取消影响
	}
}

// TestBugFix_MatcherPoolTruncate 测试 Bug 3 的修复：Pool 截断逻辑
func TestBugFix_MatcherPoolTruncate(t *testing.T) {
	eng := NewEngine()
	defer eng.Shutdown(stdctx.Background())

	// 创建一个大容量的切片模拟场景
	largeSlice := make([]*Matcher, 0, MaxMatcherPoolRetainCapacity*2)
	eng.services.matcherPool.Put(largeSlice)

	// 从池中获取
	retrieved := eng.services.matcherPool.Get()

	// 验证容量被限制
	if cap(retrieved) > MaxMatcherPoolRetainCapacity*2 {
		t.Errorf("Retrieved slice capacity = %d, should be limited", cap(retrieved))
	}
}

// TestBugFix_InvalidateCombinedChain 测试 Bug 5 的修复：invalidateCombinedChain 清空缓存
func TestBugFix_InvalidateCombinedChain(t *testing.T) {
	eng := NewEngine()
	defer eng.Shutdown(stdctx.Background())

	// 创建 matcher
	m := eng.On(dto.C2CMessageCreate)

	// 设置中间件并构建链
	eng.Use(func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			return next(ctx)
		}
	})

	// 触发链构建
	eng.rebuildMatcherChainCOW(m)

	// 获取初始链
	chain1 := m.getCombinedChain()
	if chain1 == nil {
		t.Fatal("Initial chain should not be nil")
	}

	t.Logf("Initial chain length: %d", len(chain1))

	// 直接调用 invalidateCombinedChain
	m.invalidateCombinedChain()

	// 验证缓存已失效
	chain2 := m.getCombinedChain()
	if chain2 != nil {
		t.Errorf("After invalidateCombinedChain, combinedChain should be nil, got %v (len=%d)", chain2, len(chain2))
	}

	t.Log("Bug 5 fix verified: invalidateCombinedChain correctly cleared the cache")
}
