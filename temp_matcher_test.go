package remilia

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestTempMatcher_OnTemp(t *testing.T) {
	e := NewEngine()
	// Disable auto cleaner for deterministic testing
	e.SetTempMatcherCleanInterval(0)
	defer e.Close()

	executed := false
	// Use new API OnTemp
	e.OnTemp(dto.C2CMessageCreate).Handle(func(ctx *Context) {
		executed = true
	})

	// Check it is NOT in state
	state := e.state.Load().(*engineState)
	// state.matchers might be empty or have default capacity. checking len.
	assert.Len(t, state.matchers, 0)

	// Check it IS in tempManager (Assuming we can inspect it via reflection or if Get() works)
	// Since tempManager is unexported, we can only verify via ProcessEvent or if we export Get() helper test?
	// But Get() is unexported method of internal struct.
	// Engine.ProcessEvent uses it. So if ProcessEvent works, it works.

	// Process Event
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	e.ProcessEvent(ctx)

	assert.True(t, executed)
}

func TestTempMatcher_PriorityMerge(t *testing.T) {
	e := NewEngine()
	e.SetTempMatcherCleanInterval(0)
	defer e.Close()

	var order []string

	// Perm (Priority 10)
	e.OnC2C().SetPriority(10).Handle(func(ctx *Context) {
		order = append(order, "perm_10")
	})

	// Temp (Priority 5) - Should run first
	e.OnTemp(dto.C2CMessageCreate).SetPriority(5).Handle(func(ctx *Context) {
		order = append(order, "temp_5")
	})

	// Perm (Priority 1) - Should run very first
	e.OnC2C().SetPriority(1).Handle(func(ctx *Context) {
		order = append(order, "perm_1")
	})

	// Temp (Priority 8) - Between 5 and 10
	e.OnTemp(dto.C2CMessageCreate).SetPriority(8).Handle(func(ctx *Context) {
		order = append(order, "temp_8")
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	e.ProcessEvent(ctx)

	expected := []string{"perm_1", "temp_5", "temp_8", "perm_10"}
	assert.Equal(t, expected, order)
}

func TestTempMatcher_Cleanup(t *testing.T) {
	e := NewEngine()
	e.SetTempMatcherCleanInterval(0) // disable auto cleaner
	defer e.Close()

	// 10ms expiry
	m := e.OnTemp(dto.C2CMessageCreate).SetTempWithTimeout(50 * time.Millisecond)

	executed := false
	m.Handle(func(ctx *Context) {
		executed = true
	})

	// Before expiry
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)
	e.ProcessEvent(ctx)
	assert.True(t, executed)
	executed = false

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Manually trigger clean
	e.cleanExpiredMatchers()

	// Should be gone
	e.ProcessEvent(ctx)
	assert.False(t, executed, "Matcher should be expired and removed")

	// Also cleanExpiredMatchers should set deleted=true? (If I fix it)
}

func TestTempMatcher_Delete(t *testing.T) {
	e := NewEngine()
	e.SetTempMatcherCleanInterval(0)
	defer e.Close()

	executed := false
	m := e.OnTemp(dto.C2CMessageCreate).Handle(func(ctx *Context) {
		executed = true
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// Run once
	e.ProcessEvent(ctx)
	assert.True(t, executed)
	executed = false

	// Delete
	m.Delete()

	// Run again
	e.ProcessEvent(ctx)
	assert.False(t, executed, "Deleted matcher should not run")
}
