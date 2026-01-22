package engine

import (
	stdctx "context"
	"testing"
	"time"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/metrics"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestZeroCoverageFunctions(t *testing.T) {
	eng := NewEngine()
	m := eng.OnTemp(dto.C2CMessageCreate)
	m.SetPriority(50)
	eng.UpdateTempMatcherPriority(m)
	eng.EnableGlobalMatchers(true)
	eng.EnableGlobalMatchers(false)
	collector := &metrics.Collector{}
	eng.SetMetricsCollector(collector)
	assert.Equal(t, collector, eng.GetMetricsCollector())
	eng.UpdateMatcherCommand(m)
	eng.UpdateMatcherIndex(m)
	eng.OnCommand(dto.C2CMessageCreate, "/test")
	eng.WithMatcherGroupBatch(func() {
		m2 := eng.OnC2C()
		eng.SetMatcherGroup(m2, "g", "s")
	})
	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	context := ctx.NewContext(payload, nil)
	matchers := eng.getMatchersForEvent(context)
	assert.GreaterOrEqual(t, len(matchers), 1)
	eng.SetTempMatcherCleanInterval(30 * time.Second)
	assert.Equal(t, 30*time.Second, eng.GetTempMatcherCleanInterval())
	eng.RemoveGroup("non-existent")
	ctxWithTimeout, cancel := stdctx.WithTimeout(stdctx.Background(), 1*time.Second)
	defer cancel()
	err := eng.Shutdown(ctxWithTimeout)
	assert.NoError(t, err)
}
