package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// This test ensures BasePlugin.AddMatcher does not trigger a full engine index rebuild
// (UpdateMatcherIndex) per matcher. We can't easily assert rebuildIndex calls directly,
// so we validate the observable behavior: group index is NOT updated by AddMatcher alone.
// Group membership is authoritative for middleware scoping/unloading, so plugin loader
// must set group BEFORE registration or use an explicit engine API.
func TestBasePluginAddMatcher_DoesNotRebuildEngineIndex(t *testing.T) {
	e := NewEngine()
	p := NewBasePlugin("p1")

	m := e.OnAny()
	// At this point, matcher is registered without group.
	state := e.state.Load().(*engineState)
	assert.Empty(t, state.groupIndex["p1"])

	// AddMatcher should set group via SetGroup (chain rebuild) but must not rebuild engine index.
	p.AddMatcher(m)

	state = e.state.Load().(*engineState)
	assert.Empty(t, state.groupIndex["p1"], "groupIndex should not be mutated without explicit engine index rebuild")
	assert.Equal(t, "p1", m.GetGroup())
	assert.Equal(t, "plugin:p1", m.Source)
}
