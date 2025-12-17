package remilia

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestOnEventType(t *testing.T) {
	rule := OnEventType(dto.C2CMessageCreate)

	// Test matching event
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Test non-matching event
	event2 := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnC2CMessage(t *testing.T) {
	rule := OnC2CMessage()

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	event2 := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnGroupAtMessage(t *testing.T) {
	rule := OnGroupAtMessage()

	event := &dto.Payload{
		Type: dto.GroupAtMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	event2 := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnGroupAddRobot(t *testing.T) {
	rule := OnGroupAddRobot()

	event := &dto.Payload{
		Type: dto.GroupAddRobot,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	event2 := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnGroupDelRobot(t *testing.T) {
	rule := OnGroupDelRobot()

	event := &dto.Payload{
		Type: dto.GroupDelRobot,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))
}

func TestOnCommand(t *testing.T) {
	rule := OnCommand("/test")

	// Test matching command
	detailMap := map[string]interface{}{
		"content": "/test hello",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Test matching command with leading spaces and tabs
	detailMapSpace := map[string]interface{}{
		"content": "   \t/test hello",
	}
	detailJSONSpace, _ := json.Marshal(detailMapSpace)
	eventSpace := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSONSpace,
	}
	ctxSpace := NewContext(eventSpace, nil)
	assert.True(t, rule(ctxSpace))

	// Test non-matching command
	detailMap2 := map[string]interface{}{
		"content": "hello /test",
	}
	detailJSON2, _ := json.Marshal(detailMap2)
	event2 := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON2,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnKeyword(t *testing.T) {
	rule := OnKeyword("hello")

	// Test matching keyword
	detailMap := map[string]interface{}{
		"content": "say hello world",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Leading spaces should still match keyword if present later
	detailMapSpace := map[string]interface{}{
		"content": "   say hello world",
	}
	detailJSONSpace, _ := json.Marshal(detailMapSpace)
	eventSpace := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSONSpace,
	}
	ctxSpace := NewContext(eventSpace, nil)
	assert.True(t, rule(ctxSpace))

	// Test non-matching keyword
	detailMap2 := map[string]interface{}{
		"content": "goodbye world",
	}
	detailJSON2, _ := json.Marshal(detailMap2)
	event2 := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON2,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnFullMatch(t *testing.T) {
	rule := OnFullMatch("hello world")

	// Test exact match
	detailMap := map[string]interface{}{
		"content": "hello world",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Leading spaces should be ignored for full match
	detailMapSpace := map[string]interface{}{
		"content": "   hello world",
	}
	detailJSONSpace, _ := json.Marshal(detailMapSpace)
	eventSpace := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSONSpace,
	}
	ctxSpace := NewContext(eventSpace, nil)
	assert.True(t, rule(ctxSpace))

	// Test non-exact match
	detailMap2 := map[string]interface{}{
		"content": "hello world!",
	}
	detailJSON2, _ := json.Marshal(detailMap2)
	event2 := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON2,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnPrefix(t *testing.T) {
	rule := OnPrefix("hello")

	// Test matching prefix
	detailMap := map[string]interface{}{
		"content": "hello world",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Leading spaces should be ignored for prefix
	detailMapSpace := map[string]interface{}{
		"content": "   hello world",
	}
	detailJSONSpace, _ := json.Marshal(detailMapSpace)
	eventSpace := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSONSpace,
	}
	ctxSpace := NewContext(eventSpace, nil)
	assert.True(t, rule(ctxSpace))

	// Test non-matching prefix
	detailMap2 := map[string]interface{}{
		"content": "world hello",
	}
	detailJSON2, _ := json.Marshal(detailMap2)
	event2 := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON2,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnSuffix(t *testing.T) {
	rule := OnSuffix("world")

	// Test matching suffix
	detailMap := map[string]interface{}{
		"content": "hello world",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Leading spaces should not affect suffix at end
	detailMapSpace := map[string]interface{}{
		"content": "   hello world",
	}
	detailJSONSpace, _ := json.Marshal(detailMapSpace)
	eventSpace := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSONSpace,
	}
	ctxSpace := NewContext(eventSpace, nil)
	assert.True(t, rule(ctxSpace))

	// Test non-matching suffix
	detailMap2 := map[string]interface{}{
		"content": "world hello",
	}
	detailJSON2, _ := json.Marshal(detailMap2)
	event2 := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON2,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))
}

func TestOnRegex(t *testing.T) {
	t.Run("匹配数字", func(t *testing.T) {
		rule := OnRegex(`\d+`)

		detailMap := map[string]interface{}{
			"content": "hello 123",
		}
		detailJSON, _ := json.Marshal(detailMap)
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailJSON,
		}
		ctx := NewContext(event, nil)
		assert.True(t, rule(ctx))
	})

	t.Run("不匹配数字", func(t *testing.T) {
		rule := OnRegex(`\d+`)

		detailMap := map[string]interface{}{
			"content": "hello world",
		}
		detailJSON, _ := json.Marshal(detailMap)
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailJSON,
		}
		ctx := NewContext(event, nil)
		assert.False(t, rule(ctx))
	})

	t.Run("匹配邮箱", func(t *testing.T) {
		rule := OnRegex(`^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$`)

		detailMap := map[string]interface{}{
			"content": "user@example.com",
		}
		detailJSON, _ := json.Marshal(detailMap)
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailJSON,
		}
		ctx := NewContext(event, nil)
		assert.True(t, rule(ctx))
	})
}

func TestOnRegexSafe(t *testing.T) {
	t.Run("有效正则", func(t *testing.T) {
		rule, err := OnRegexSafe(`\d+`)
		assert.NoError(t, err)
		assert.NotNil(t, rule)

		detailMap := map[string]interface{}{
			"content": "test 123",
		}
		detailJSON, _ := json.Marshal(detailMap)
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailJSON,
		}
		ctx := NewContext(event, nil)
		assert.True(t, rule(ctx))
	})

	t.Run("无效正则", func(t *testing.T) {
		t.Parallel()
		rule, err := OnRegexSafe(`[invalid(`)
		assert.Error(t, err)
		assert.Nil(t, rule)
	})
}

func TestOnRegexCompiled(t *testing.T) {
	re := regexp.MustCompile(`\d+`)
	rule := OnRegexCompiled(re)

	detailMap := map[string]interface{}{
		"content": "test 456",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))
}

func TestAnd(t *testing.T) {
	t.Parallel()
	rule := And(
		func(ctx *Context) bool { return true },
		func(ctx *Context) bool { return true },
		func(ctx *Context) bool { return true },
	)

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx), "And should return true when all rules pass")
}

func TestAnd_OneFails(t *testing.T) {
	rule := And(
		func(ctx *Context) bool { return true },
		func(ctx *Context) bool { return false },
		func(ctx *Context) bool { return true },
	)

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.False(t, rule(ctx), "And should return false when any rule fails")
}

func TestAnd_Empty(t *testing.T) {
	t.Parallel()
	rule := And()

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx), "And with no rules should return true")
}

func TestOr(t *testing.T) {
	rule := Or(
		func(ctx *Context) bool { return false },
		func(ctx *Context) bool { return true },
		func(ctx *Context) bool { return false },
	)

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx), "Or should return true when at least one rule passes")
}

func TestOr_AllFail(t *testing.T) {
	rule := Or(
		func(ctx *Context) bool { return false },
		func(ctx *Context) bool { return false },
		func(ctx *Context) bool { return false },
	)

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.False(t, rule(ctx), "Or should return false when all rules fail")
}

func TestOr_Empty(t *testing.T) {
	t.Parallel()
	rule := Or()

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := NewContext(event, nil)
	assert.False(t, rule(ctx), "Or with no rules should return false")
}

func TestComplexRuleCombination(t *testing.T) {
	t.Parallel()
	// Test complex combination: (C2C AND (keyword OR command)) AND NOT suffix
	rule := And(
		OnC2CMessage(),
		Or(
			OnKeyword("hello"),
			OnCommand("/start"),
		),
		Not(OnSuffix("!")),
	)

	// Should match: C2C message with "hello" and no "!" at end
	detailMap := map[string]interface{}{
		"content": "hello world",
	}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
	ctx := NewContext(event, nil)
	assert.True(t, rule(ctx))

	// Should not match: has "!" at end
	detailMap2 := map[string]interface{}{
		"content": "hello world!",
	}
	detailJSON2, _ := json.Marshal(detailMap2)
	event2 := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON2,
	}
	ctx2 := NewContext(event2, nil)
	assert.False(t, rule(ctx2))

	// Should not match: Group message
	event3 := &dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: detailJSON,
	}
	ctx3 := NewContext(event3, nil)
	assert.False(t, rule(ctx3))
}
