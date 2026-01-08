package remilia

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// Regression test: WithMatcherGroupBatch must NOT deadlock when the callback
// registers matchers (Engine.On/OnAny/OnCommand) which internally take writeMu.
//
// This used to deadlock because WithMatcherGroupBatch held writeMu while executing fn.
func TestEngineWithMatcherGroupBatch_NoDeadlockWithOn(t *testing.T) {
	e := NewEngine()
	p := NewBasePlugin("p")

	done := make(chan struct{})
	go func() {
		e.WithMatcherGroupBatch(func() {
			for i := 0; i < 50; i++ {
				m := e.On(dto.C2CMessageCreate, func(ctx *Context) bool { return true })
				m.Handle(func(ctx *Context) {})
				p.AddMatcher(m)
			}
		})
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("WithMatcherGroupBatch appears to deadlock when callback registers matchers")
	}

	// Sanity checks: matchers registered, and group fields set.
	assert.Equal(t, 50, e.GetMatcherCount())
	for _, m := range p.GetMatchers() {
		if m == nil {
			continue
		}
		assert.Equal(t, "p", m.GetGroup())
		assert.Equal(t, "plugin:p", m.Source)
	}
}
