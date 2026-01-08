package remilia

import (
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestMatcherMatch_TypeOnly(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
	}

	// Test matching event
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, matcher.Match(ctx))

	// Test non-matching event
	event2 := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
	}
	ctx2 := NewContext(event2, nil)
	// 浜嬩欢绫诲瀷杩囨护宸茬粡鍦?Engine.getMatchersForEvent 涓畬鎴愶紝杩欓噷 Match 鍙鏌ヨ鍒?	// 鍥犳瀵逛簬涓嶅悓浜嬩欢绫诲瀷锛孧atch 浠嶇劧杩斿洖 true
	assert.True(t, matcher.Match(ctx2))
}

func TestMatcherMatch_WithRules(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []Rule{
			func(ctx *Context) bool { return true },
			func(ctx *Context) bool { return true },
		},
	}

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, matcher.Match(ctx))
}

func TestMatcherMatch_RuleFails(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []Rule{
			func(ctx *Context) bool { return true },
			func(ctx *Context) bool { return false }, // This rule fails
		},
	}

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.False(t, matcher.Match(ctx))
}

func TestMatcherMatch_NoRules(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []Rule{},
	}

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, matcher.Match(ctx))
}

func TestMatcherHandle(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{}

	handler := func(ctx *Context) {}
	result := matcher.Handle(handler)

	assert.NotNil(t, matcher.Handler)
	assert.Nil(t, matcher.HandlerErr)
	assert.Equal(t, matcher, result) // Test method chaining
}

func TestMatcherHandleE(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{}

	handler := func(ctx *Context) error { return nil }
	result := matcher.HandleE(handler)

	assert.NotNil(t, matcher.HandlerErr)
	assert.Nil(t, matcher.Handler)
	assert.Equal(t, matcher, result)
}

func TestMatcherSetPriority(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{}

	result := matcher.SetPriority(10)

	assert.Equal(t, uint(10), matcher.priority)
	assert.Equal(t, matcher, result) // Test method chaining
}

func TestMatcherSetBlock(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{}

	result := matcher.SetBlock(true)

	assert.True(t, matcher.isBlock)
	assert.Equal(t, matcher, result) // Test method chaining

	matcher.SetBlock(false)
	assert.False(t, matcher.isBlock)
}

func TestMatcherSetTemp(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{}

	matcher.SetTemp(true)

	assert.True(t, matcher.IsTemp())

	matcher.SetTemp(false)
	assert.False(t, matcher.IsTemp())
}

func TestMatcherCopy(t *testing.T) {
	t.Parallel()
	handler := func(ctx *Context) {}
	handlerE := func(ctx *Context) error { return nil }
	engine := NewEngine()

	original := &Matcher{
		EventType:   dto.C2CMessageCreate,
		Rules:       []Rule{func(ctx *Context) bool { return true }},
		isBlock:     true,
		priority:    10,
		Handler:     handler,
		HandlerErr:  handlerE,
		coordinator: engine,
		rt:          matcherRuntime{isTemp: 1},
		Source:      "global",
	}

	copied := original.copy()

	assert.NotEqual(t, original, copied) // Different instances
	assert.Equal(t, original.EventType, copied.EventType)
	assert.Equal(t, original.isBlock, copied.isBlock)
	assert.Equal(t, original.priority, copied.priority)
	assert.Equal(t, original.rt.isTemp, copied.rt.isTemp)
	assert.Equal(t, original.Source, copied.Source)
	// assert.Equal on slice of functions fails if backing arrays differ (DeepEqual limitation)
	assert.Equal(t, len(original.Rules), len(copied.Rules))
	if len(original.Rules) > 0 {
		// Verify deep copy: backing arrays should be different
		assert.NotEqual(t, fmt.Sprintf("%p", original.Rules), fmt.Sprintf("%p", copied.Rules))
	}
	assert.NotNil(t, copied.Handler)
	assert.NotNil(t, copied.HandlerErr)
}

func TestMatcherDelete(t *testing.T) {
	t.Parallel()
	engine := NewEngine()

	matcher := engine.OnC2C()

	state := engine.state.Load().(*engineState)
	initialCount := len(state.matchers)

	assert.Equal(t, 1, initialCount)

	matcher.Delete()

	state = engine.state.Load().(*engineState)
	finalCount := len(state.matchers)

	assert.Equal(t, 0, finalCount)
}

func TestMatcherDelete_NotInList(t *testing.T) {
	t.Parallel()
	engine := NewEngine()

	matcher := &Matcher{coordinator: engine}

	assert.NotPanics(t, func() {
		matcher.Delete()
	})

	orphanMatcher := &Matcher{}
	assert.NotPanics(t, func() {
		orphanMatcher.Delete()
	})
}

func TestMatcherChaining(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{}

	// Test method chaining
	result := matcher.
		SetPriority(5).
		SetBlock(true).
		SetTemp(true).
		Handle(func(ctx *Context) {})

	assert.Equal(t, matcher, result)
	assert.Equal(t, uint(5), matcher.priority)
	assert.True(t, matcher.isBlock)
	assert.True(t, matcher.IsTemp())
	assert.NotNil(t, matcher.Handler)
}

func TestMatcherMatch_EmptyRules(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules:     []Rule{},
	}

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, matcher.Match(ctx))
}

func TestMatcherMatch_NilEventType(t *testing.T) {
	t.Parallel()
	matcher := &Matcher{
		// EventType 涓虹┖琛ㄧず鐢变笂灞傜储寮曟帶鍒舵槸鍚﹀弬涓庡尮閰?		Rules: []Rule{},
	}

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, matcher.Match(ctx), "Matcher with empty EventType should match based on rules only")
}
