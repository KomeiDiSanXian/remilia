package engine

import (
	stdctx "context"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestEdgeCases(t *testing.T) {
	eng := NewEngine()
	eng.RemoveGroup("")
	eng.SetMaxMatchers(100)
	for range 5 {
		eng.OnC2C()
	}
	assert.Equal(t, 5, eng.GetMatcherCount())
	stats := eng.GetMatcherStats()
	assert.Equal(t, 5, stats.Total)
	ctx1, cancel := stdctx.WithTimeout(stdctx.Background(), 100*time.Millisecond)
	defer cancel()
	err := eng.Shutdown(ctx1)
	assert.NoError(t, err)
}
func TestMatcherEdges(t *testing.T) {
	eng := NewEngine()
	m := eng.OnC2C()
	m.Delete()
	assert.True(t, m.IsDeleted())
	m2 := eng.OnC2C()
	m2.rt.deleted = true
	matched := m2.Match(ctx.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil))
	assert.False(t, matched)
	m3 := eng.OnC2C()
	m3.Handle(nil)
	m3.SetPriority(10)
	m3.SetPriority(10)
	m3.SetBlock(true)
	m3.SetBlock(true)
	m3.SetTemp(true)
	m3.SetTemp(true)
	assert.True(t, m3.IsTemp())
}
func TestRestoreEdge(t *testing.T) {
	eng := NewEngine()
	eng.OnC2C()
	snap := eng.Snapshot()
	eng.DeleteAllMatchers()
	eng.Restore(snap)
	assert.Equal(t, 1, eng.GetMatcherCount())
}
