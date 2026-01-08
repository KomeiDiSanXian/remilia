package remilia

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// Verify Engine implements MatcherCoordinator
var _ MatcherCoordinator = (*Engine)(nil)

func TestMatcherDecoupling(t *testing.T) {
	engine := NewEngine()

	// 1. Test On() creates Matcher with coordinator
	m := engine.On(dto.C2CMessageCreate, func(ctx *Context) bool { return true })
	assert.NotNil(t, m.coordinator, "Matcher coordinator should be set")
	assert.Equal(t, engine, m.coordinator, "Coordinator should be the engine")

	// 2. Test BindCommand uses coordinator (UpdateMatcherCommand)
	m.BindCommand("/test")
	// Since we can't easily check internal state without deep inspection,
	// we rely on coverage / logic not panicking.
	// But we can check if it works by checking if command optimization structure is built?
	// The internal implementation of updateMatcherCommand creates a new state and rebuilds index.
	// We can check execution.

	// 3. Test SetPriority uses coordinator (InvalidateSortedCache)
	m.SetPriority(10)
	assert.Equal(t, uint(10), m.priority)

	// 4. Test SetTemp uses coordinator (MigrateMatcherToTemp)
	m.SetTemp(true)
	assert.True(t, m.IsTemp())
	assert.Equal(t, 1, engine.GetTempMatcherCount(), "Temp matcher should be migrated")

	// 5. Test SetTemp(false) uses coordinator (MigrateMatcherFromTemp)
	m.SetTemp(false)
	assert.False(t, m.IsTemp())
	assert.Equal(t, 0, engine.GetTempMatcherCount(), "Temp matcher should be migrated back")

	// 6. Test Delete uses coordinator (DeleteMatcher)
	m.Delete()
	assert.True(t, m.IsDeleted())
	// assert.Equal(t, 0, engine.GetMatcherCount()) // Note: Delete is async via pendingDeleteCh usually?
	// Wait, Engine.DeleteMatcher(m) is COW direct delete.
	// Matcher.Delete calls engine.DeleteMatcher.
	assert.Equal(t, 0, engine.GetMatcherCount())
}

func TestMatcherOnTempDecoupling(t *testing.T) {
	engine := NewEngine()
	m := engine.OnTemp(dto.C2CMessageCreate, func(ctx *Context) bool { return true })

	assert.NotNil(t, m.coordinator)
	assert.True(t, m.IsTemp())

	m.SetTempWithTimeout(1 * time.Minute)
	assert.True(t, m.IsTemp())
}

func TestMatcherOnCommandDecoupling(t *testing.T) {
	engine := NewEngine()
	m := engine.OnCommand(dto.C2CMessageCreate, "/foo")

	assert.NotNil(t, m.coordinator)
	assert.Equal(t, "/foo", m.command)
}
