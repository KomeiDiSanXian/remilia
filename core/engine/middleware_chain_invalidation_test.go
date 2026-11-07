package engine

// middleware_chain_invalidation_test.go — 组合中间件链的缓存失效
//
// 语义：invalidateCombinedChain 必须同时清空 combinedChain 并自增
// compiledVersion，否则 invokeHandler 的快路径会继续复用旧链。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestMatcher_InvalidateCombinedChainBumpsVersion 验证失效后 combinedChain
// 被清空且 compiledVersion 自增（否则编译链快路径会继续复用旧链）。
func TestMatcher_InvalidateCombinedChainBumpsVersion(t *testing.T) {
	eng := newEngineForTest(t)

	m := eng.On(string(platform.EventKindPrivateMessage))

	// 设置中间件并构建链
	eng.Use(func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			return next(ctx)
		}
	})
	eng.rebuildMatcherChainCOW(m)

	chain1 := m.getCombinedChain()
	if chain1 == nil {
		t.Fatal("Initial chain should not be nil")
	}

	verBefore := m.compiledVersion.Load()
	m.invalidateCombinedChain()

	if chain2 := m.getCombinedChain(); chain2 != nil {
		t.Errorf("After invalidateCombinedChain, combinedChain should be nil, got %v (len=%d)", chain2, len(chain2))
	}
	if got := m.compiledVersion.Load(); got <= verBefore {
		t.Errorf("compiledVersion should be bumped so the compiled handler chain is rebuilt: %d -> %d", verBefore, got)
	}
}
