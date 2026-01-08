package remilia

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
)

func TestMatcher_SetGroup_DrivesGroupMiddleware(t *testing.T) {
	engine := NewEngine()

	// group middleware
	engine.UseForGroup("g1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			ctx.SetState("mw_hit", true)
			return next(ctx)
		}
	})

	var hit int32
	m := engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt32(&hit, 1)
	})

	// initially no group
	ctx1 := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx1)
	_, ok := ctx1.GetState("mw_hit")
	require.False(t, ok)

	// set group -> middleware should run
	m.SetGroup("g1")
	ctx2 := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx2)
	v, ok := ctx2.GetState("mw_hit")
	require.True(t, ok)
	require.Equal(t, true, v)
}

func TestMatcher_SetGroup_EmptyClearsGroup(t *testing.T) {
	engine := NewEngine()
	engine.UseForGroup("g1", func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			ctx.SetState("mw_hit", true)
			return next(ctx)
		}
	})

	m := engine.OnC2C().Handle(func(ctx *Context) {})
	m.SetGroup("g1")

	ctx1 := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx1)
	_, ok := ctx1.GetState("mw_hit")
	require.True(t, ok)

	m.SetGroup("")
	ctx2 := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx2)
	_, ok = ctx2.GetState("mw_hit")
	require.False(t, ok)
}
