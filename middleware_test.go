package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestEngine_Middleware_OrderAndError(t *testing.T) {
	engine := NewEngine()
	var order []int
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			order = append(order, 1)
			return next(ctx)
		}
	}, func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			order = append(order, 2)
			return next(ctx)
		}
	})

	// 使用新 API：OnC2C 注册处理器
	engine.OnC2C().HandleE(func(ctx *Context) error { return assert.AnError })
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// middlewares execute in registration order
	assert.Equal(t, []int{1, 2}, order)
	// 注意：错误处理现在通过中间件实现，不再有全局 AddErrorHandler
}

func TestEngine_Middleware_AdapterHandle(t *testing.T) {
	engine := NewEngine()
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error { ctx.SetState("mw", true); return next(ctx) }
	})
	var seen bool
	// 使用新 API：OnC2C 注册处理器
	engine.OnC2C().Handle(func(ctx *Context) {
		if v, ok := ctx.GetState("mw"); ok {
			seen = v.(bool)
		}
	})
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)
	assert.True(t, seen)
}
