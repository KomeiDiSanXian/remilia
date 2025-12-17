package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestEngine_MaxMatchersReturnsNoopMatcher 测试达到匹配器限制时返回 noopMatcher 而非 nil
func TestEngine_MaxMatchersReturnsNoopMatcher(t *testing.T) {
	engine := NewEngine()
	engine.SetMaxMatchers(2)

	// 注册两个正常的 matcher
	m1 := engine.OnC2C().Handle(func(ctx *Context) {
		ctx.SetState("m1", true)
	})
	assert.NotNil(t, m1)
	assert.NotEqual(t, noopMatcher, m1)

	m2 := engine.OnC2C().Handle(func(ctx *Context) {
		ctx.SetState("m2", true)
	})
	assert.NotNil(t, m2)
	assert.NotEqual(t, noopMatcher, m2)

	// 第三个应该返回 noopMatcher
	m3 := engine.OnC2C().Handle(func(ctx *Context) {
		ctx.SetState("m3", true)
	})
	assert.NotNil(t, m3, "Should return noopMatcher, not nil")
	assert.Equal(t, noopMatcher, m3, "Should return noopMatcher when limit reached")

	// noopMatcher 的链式调用应该安全
	m4 := m3.Command("/test").Keyword("hello").SetPriority(10).SetBlock(true).SetTemp(true)
	assert.Equal(t, noopMatcher, m4, "Chained calls on noopMatcher should return itself")

	// 验证只有 m1 和 m2 真正注册了
	assert.Equal(t, 2, engine.GetMatcherCount())

	// 测试事件处理，m3 不应被执行
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// 应该只有 m1 和 m2 执行了
	_, ok1 := ctx.GetState("m1")
	_, ok2 := ctx.GetState("m2")
	_, ok3 := ctx.GetState("m3")

	assert.True(t, ok1, "m1 should have executed")
	assert.True(t, ok2, "m2 should have executed")
	assert.False(t, ok3, "m3 (noopMatcher) should not have executed")
}

// TestNoopMatcher_SafeChaining 测试 noopMatcher 的所有链式方法都安全
func TestNoopMatcher_SafeChaining(t *testing.T) {
	m := noopMatcher

	// 测试所有链式方法不会 panic
	assert.NotPanics(t, func() {
		m.Handle(func(ctx *Context) {}).
			HandleE(func(ctx *Context) error { return nil }).
			Command("/test").
			Keyword("hello").
			Prefix("pre").
			Suffix("suf").
			FullMatch("exact").
			Regex(".*").
			Where(func(ctx *Context) bool { return true }).
			SetPriority(10).
			SetBlock(true).
			SetTemp(true).
			SetTempWithMaxUse(5).
			Use(func(next HandlerE) HandlerE {
				return func(ctx *Context) error {
					return next(ctx)
				}
			})
	})

	// 所有方法都应该返回 noopMatcher 自身
	result := m.Handle(func(ctx *Context) {})
	assert.Equal(t, noopMatcher, result)
}

// TestNoopMatcher_Match 测试 noopMatcher 永远不匹配
func TestNoopMatcher_Match(t *testing.T) {
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	// noopMatcher 已标记为 deleted，不应该匹配
	matched := noopMatcher.Match(ctx)
	assert.False(t, matched, "noopMatcher should never match (deleted=true)")
}
