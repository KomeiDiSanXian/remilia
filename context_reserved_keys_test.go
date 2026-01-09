package remilia

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContext_SetState_ReservedKeyIsForbidden(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.Set("mw_trace", []string{"mw1"})

	_, ok := ctx.Get("mw_trace")
	assert.False(t, ok, "legacy key mw_trace must be forbidden in user state")
}

func TestContext_SetState_InternalPrefixIsForbidden(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.Set("_remilia_internal_x", 1)

	_, ok := ctx.Get("_remilia_internal_x")
	assert.False(t, ok, "_remilia_internal_* keys must be forbidden in user state")
}

func TestContext_SetStateMap_FiltersReservedKeys(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.Set("mw_trace", []string{"mw1"})
	ctx.Set("_remilia_internal_x", 1)
	ctx.Set("user_ok", 123)

	_, ok := ctx.Get("mw_trace")
	assert.False(t, ok)
	_, ok = ctx.Get("_remilia_internal_x")
	assert.False(t, ok)

	v, ok := ctx.Get("user_ok")
	assert.True(t, ok)
	assert.Equal(t, 123, v)
}
