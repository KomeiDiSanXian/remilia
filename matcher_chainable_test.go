package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestMatcherChainableCommand(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnGroupAt().
		Command("/test").
		Handle(func(ctx *Context) {
			executed = true
		})

	// 匹配
	ctx := NewContext(&dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: []byte(`{"content":"/test hello"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)

	// 不匹配
	executed = false
	ctx2 := NewContext(&dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: []byte(`{"content":"hello"}`),
	}, nil)
	engine.ProcessEvent(ctx2)
	assert.False(t, executed)
}

func TestMatcherChainableKeyword(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnC2C().
		Keyword("hello").
		Handle(func(ctx *Context) {
			executed = true
		})

	// 匹配
	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"say hello world"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)
}

func TestMatcherChainableMultipleConditions(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnGroupAt().
		Command("/admin").
		Keyword("delete").
		Handle(func(ctx *Context) {
			executed = true
		})

	// 匹配：同时满足两个条件
	ctx := NewContext(&dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: []byte(`{"content":"/admin delete user"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)

	// 不匹配：只满足一个条件
	executed = false
	ctx2 := NewContext(&dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: []byte(`{"content":"/admin update user"}`),
	}, nil)
	engine.ProcessEvent(ctx2)
	assert.False(t, executed)
}

func TestMatcherChainablePrefix(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnC2C().
		Prefix("!").
		Handle(func(ctx *Context) {
			executed = true
		})

	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"!help"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)
}

func TestMatcherChainableSuffix(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnC2C().
		Suffix("?").
		Handle(func(ctx *Context) {
			executed = true
		})

	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"what is this?"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)
}

func TestMatcherChainableFullMatch(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnGroupAt().
		FullMatch("ping").
		Handle(func(ctx *Context) {
			executed = true
		})

	// 匹配
	ctx := NewContext(&dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: []byte(`{"content":"ping"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)

	// 不匹配
	executed = false
	ctx2 := NewContext(&dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: []byte(`{"content":"ping pong"}`),
	}, nil)
	engine.ProcessEvent(ctx2)
	assert.False(t, executed)
}

func TestMatcherChainableRegex(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnC2C().
		Regex(`^\d{3}-\d{4}$`).
		Handle(func(ctx *Context) {
			executed = true
		})

	// 匹配
	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"123-4567"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)

	// 不匹配
	executed = false
	ctx2 := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"abc-defg"}`),
	}, nil)
	engine.ProcessEvent(ctx2)
	assert.False(t, executed)
}

func TestMatcherChainableWhere(t *testing.T) {
	engine := NewEngine()

	executed := false
	customRule := func(ctx *Context) bool {
		content := ctx.GetMessageContent()
		return len(content) > 10
	}

	engine.OnC2C().
		Where(customRule).
		Handle(func(ctx *Context) {
			executed = true
		})

	// 匹配
	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"this is a long message"}`),
	}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, executed)

	// 不匹配
	executed = false
	ctx2 := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content":"short"}`),
	}, nil)
	engine.ProcessEvent(ctx2)
	assert.False(t, executed)
}

func TestMatcherChainableVsTraditional(t *testing.T) {
	engine := NewEngine()

	executed := false
	engine.OnGroupAt().
		Command("/ping").
		Keyword("urgent").
		Handle(func(ctx *Context) {
			executed = true
		})

	ctx := NewContext(&dto.Payload{
		Type:   dto.GroupAtMessageCreate,
		Detail: []byte(`{"content":"/ping urgent message"}`),
	}, nil)
	engine.ProcessEvent(ctx)

	assert.True(t, executed, "链式调用应该匹配")
}
