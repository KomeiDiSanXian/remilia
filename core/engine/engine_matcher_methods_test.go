package engine

import (
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestMatcherBuilders(t *testing.T) {
	eng := NewEngine()
	m := eng.On(dto.C2CMessageCreate)
	m.Use(func(next ctx.Handler) ctx.Handler { return next })
	m.Command("/test")
	m.Keyword("hello")
	m.Prefix("!")
	m.Suffix("?")
	m.FullMatch("exact")
	m.Regex(`\d+`)
	m.Where(func(c *ctx.Context) bool { return true })
	m.BindCommand("/bind")
	m.SetGroup("group")
	assert.Equal(t, "group", m.GetGroup())
	m.SetTempWithMaxUse(3)
	m.SetTempWithTimeout(5 * time.Second)
	assert.NotNil(t, m)
}
func TestMiddlewareExtra(t *testing.T) {
	eng := NewEngine()
	eng.Named("test", func(next ctx.Handler) ctx.Handler { return next })
	eng.ResetMiddlewares()
	eng.SetMiddlewareTraceHook(func(name string, c *ctx.Context) {})
	eng.EnableMiddlewareTrace()
	eng.SetTempMatcherCleanInterval(10 * time.Second)
	assert.Equal(t, 10*time.Second, eng.GetTempMatcherCleanInterval())
}
func TestMatcherInternal(t *testing.T) {
	eng := NewEngine()
	m := eng.On(dto.C2CMessageCreate)
	assert.False(t, m.deletedOrLocked())
	m.invalidateCombinedChain()
	m.setCombinedChain(nil, 0, 0)
	copied := m.copy()
	assert.NotNil(t, copied)
	c := ctx.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	matchers := eng.getMatchersForEvent(c)
	assert.GreaterOrEqual(t, len(matchers), 1)
}
