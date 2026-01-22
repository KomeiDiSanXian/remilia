package engine

import (
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestRemainingFunctions(t *testing.T) {
	eng := NewEngine()
	m := eng.OnTemp(dto.C2CMessageCreate)
	eng.UpdateTempMatcherPriority(m)
	eng.UpdateMatcherCommand(m)
	eng.UpdateMatcherIndex(m)
	eng.EnableGlobalMatchers(true)
	eng.SetMetricsCollector(nil)
	assert.Nil(t, eng.GetMetricsCollector())
	eng.OnCommand(dto.C2CMessageCreate, "/test")
	eng.WithMatcherGroupBatch(func() {
		m2 := eng.OnC2C()
		eng.SetMatcherGroup(m2, "g", "s")
	})
	assert.NotNil(t, eng)
}
func TestProcessInternals(t *testing.T) {
	eng := NewEngine()
	ctx := ctx.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	matchers := eng.getMatchersForEvent(ctx)
	assert.NotNil(t, matchers)
}
