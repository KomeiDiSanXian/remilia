package engine

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestAsyncComponents(t *testing.T) {
	eng := NewEngine(WithCleanupInterval(50*time.Millisecond), WithPendingDeleteBufferSize(10))
	for i := 0; i < 5; i++ {
		m := eng.OnTemp(dto.C2CMessageCreate)
		m.rt.expiresAt = time.Now().Add(-1 * time.Second)
	}
	time.Sleep(150 * time.Millisecond)
	for i := 0; i < 10; i++ {
		m := eng.OnC2C()
		eng.DeleteMatcher(m)
	}
	time.Sleep(100 * time.Millisecond)
	c, cancel := stdctx.WithTimeout(stdctx.Background(), 500*time.Millisecond)
	defer cancel()
	err := eng.Shutdown(c)
	assert.NoError(t, err)
}
func TestRemoveGroupBranches(t *testing.T) {
	eng := NewEngine()
	eng.RemoveGroup("empty-before-add")
	eng.WithMatcherGroupBatch(func() {
		for i := 0; i < 5; i++ {
			m := eng.OnC2C()
			eng.SetMatcherGroup(m, "large-group", "src")
		}
	})
	eng.RemoveGroup("large-group")
	assert.Equal(t, 0, eng.GetMatcherCount())
}
func TestInvokeHandlerPaths(t *testing.T) {
	eng := NewEngine()
	eng.OnC2C().Handle(func(c *ctx.Context) error {
		return assert.AnError
	})
	payload := &dto.Payload{Type: dto.C2CMessageCreate}
	context := ctx.NewContext(payload, nil)
	eng.ProcessEvent(context)
	eng2 := NewEngine()
	m := eng2.OnC2C()
	m.Handler = nil
	eng2.ProcessEvent(ctx.NewContext(payload, nil))
}
func TestStateCopyEdges(t *testing.T) {
	state := newEngineState()
	for i := 0; i < 10; i++ {
		m := &Matcher{EventType: dto.C2CMessageCreate, priority: uint(i * 10), group: "g1"}
		if i%3 == 0 {
			m.definition = &command.Definition{Name: "cmd"}
		}
		state.addMatcher(m)
	}
	copied := copyEngineState(state)
	assert.Equal(t, len(state.matchers), len(copied.matchers))
	assert.Equal(t, len(state.groupIndex), len(copied.groupIndex))
	assert.Equal(t, len(state.commandIndex), len(copied.commandIndex))
}
