package remilia

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestMiddlewareTraceHook_CalledForNamedMiddleware(t *testing.T) {
	engine := NewEngine()

	var called atomic.Int32
	engine.SetMiddlewareTraceHook(func(name string, ctx *Context) {
		assert.Equal(t, "mw1", name)
		assert.NotNil(t, ctx)
		called.Add(1)
	})

	engine.Use(engine.Named("mw1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	}))

	engine.OnC2C().HandleE(func(ctx *Context) error { return nil })
	engine.ProcessEvent(NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil))

	assert.Equal(t, int32(1), called.Load())
}

func TestMiddlewareTraceHook_DisabledDoesNotCall(t *testing.T) {
	engine := NewEngine()

	var called atomic.Int32
	engine.SetMiddlewareTraceHook(nil)

	engine.Use(engine.Named("mw1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	}))

	engine.OnC2C().HandleE(func(ctx *Context) error { return nil })
	engine.ProcessEvent(NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil))

	assert.Equal(t, int32(0), called.Load())
}

func TestMiddlewareTraceHook_PanicIsRecovered(t *testing.T) {
	engine := NewEngine()

	engine.SetMiddlewareTraceHook(func(name string, ctx *Context) {
		panic("boom")
	})

	var handlerCalled atomic.Bool
	engine.Use(engine.Named("mw1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	}))
	engine.OnC2C().HandleE(func(ctx *Context) error {
		handlerCalled.Store(true)
		return nil
	})

	engine.ProcessEvent(NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil))
	assert.True(t, handlerCalled.Load())
}

func TestMiddlewareTraceHook_EnableLegacyMwTraceState(t *testing.T) {
	engine := NewEngine()
	engine.EnableMiddlewareTrace()

	engine.Use(engine.Named("mw1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			return next(ctx)
		}
	}))

	engine.OnC2C().HandleE(func(ctx *Context) error { return nil })
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	v, ok := ctx.internalGet(internalStateKeyMiddlewareTrace)
	assert.True(t, ok)
	assert.Equal(t, []string{"mw1"}, v)

	// Legacy userState key must not be written.
	_, ok = ctx.GetState("mw_trace")
	assert.False(t, ok)
}
