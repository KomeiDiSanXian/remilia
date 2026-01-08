package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEngine_RemoveGroup(t *testing.T) {
	e := NewEngine()

	// We need a way to register a matcher with group.
	// We can manually use registerMatcher
	createAndRegister := func(group string, id string) *Matcher {
		m := &Matcher{
			Source: id,
			group:  group,
		}
		e.registerMatcher(m)
		return m
	}

	m1 := createAndRegister("groupA", "m1")
	m2 := createAndRegister("groupA", "m2")
	m3 := createAndRegister("groupB", "m3")

	// Check identifiers to ensure they are created correctly
	assert.Equal(t, "m1", m1.Source)
	assert.Equal(t, "m2", m2.Source)

	// Verify initial state
	state := e.state.Load().(*engineState)
	assert.Len(t, state.matchers, 3)
	assert.Len(t, state.groupIndex["groupA"], 2)
	assert.Len(t, state.groupIndex["groupB"], 1)

	// Remove Group A
	e.RemoveGroup("groupA")

	// Verify state after removal
	state = e.state.Load().(*engineState)
	assert.Len(t, state.matchers, 1)
	assert.Equal(t, m3, state.matchers[0])
	assert.Empty(t, state.groupIndex["groupA"])
	assert.Len(t, state.groupIndex["groupB"], 1)

	// Remove Group B
	e.RemoveGroup("groupB")
	state = e.state.Load().(*engineState)
	assert.Empty(t, state.matchers)
	assert.Empty(t, state.groupIndex["groupB"])
}

func TestEngine_RemoveGroup_NonExistent(t *testing.T) {
	e := NewEngine()
	m1 := &Matcher{group: "groupA"}
	e.registerMatcher(m1)

	e.RemoveGroup("groupNonExistent")

	state := e.state.Load().(*engineState)
	assert.Len(t, state.matchers, 1)
}

func TestEngine_GroupIndex_UpdatedOnAdd(t *testing.T) {
	e := NewEngine()
	m1 := &Matcher{group: "g1"}
	e.registerMatcher(m1)

	state := e.state.Load().(*engineState)
	assert.Equal(t, []*Matcher{m1}, state.groupIndex["g1"])
}

func TestEngine_DeleteMatcher_UpdatesGroupIndex(t *testing.T) {
	e := NewEngine()
	m1 := &Matcher{group: "g1"}
	e.registerMatcher(m1)

	e.DeleteMatcher(m1)

	state := e.state.Load().(*engineState)
	assert.Empty(t, state.groupIndex["g1"])
}

func TestEngine_UpdateMatcherIndex(t *testing.T) {
	e := NewEngine()
	// Register normally (global, no group)
	m := e.OnAny(OnFullMatch("test"))

	// Modify group (need access to private field or helper)
	// Since I am in same package, I can set m.group
	m.group = "newGroup"

	// Verify NOT in index yet
	state := e.state.Load().(*engineState)
	assert.Empty(t, state.groupIndex["newGroup"])

	// Call UpdateMatcherIndex
	e.UpdateMatcherIndex(m)

	// Verify IS in index
	state = e.state.Load().(*engineState)
	assert.Len(t, state.groupIndex["newGroup"], 1)
	assert.Equal(t, m, state.groupIndex["newGroup"][0])
}
