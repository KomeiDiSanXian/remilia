package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContext_SetState_ReservedKeyIsForbidden(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.SetState("mw_trace", []string{"mw1"})

	_, ok := ctx.GetState("mw_trace")
	assert.False(t, ok, "legacy key mw_trace must be forbidden in user state")
}

func TestContext_SetState_InternalPrefixIsForbidden(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.SetState("_remilia_internal_x", 1)

	_, ok := ctx.GetState("_remilia_internal_x")
	assert.False(t, ok, "_remilia_internal_* keys must be forbidden in user state")
}

func TestContext_SetStateMap_FiltersReservedKeys(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.SetStateMap(State{
		"mw_trace":            []string{"mw1"},
		"_remilia_internal_x": 1,
		"user_ok":             123,
	})

	_, ok := ctx.GetState("mw_trace")
	assert.False(t, ok)
	_, ok = ctx.GetState("_remilia_internal_x")
	assert.False(t, ok)

	v, ok := ctx.GetState("user_ok")
	assert.True(t, ok)
	assert.Equal(t, 123, v)
}
