package remilia

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContext_SetGetAll_V2Sugar(t *testing.T) {
	ctx := NewContext(nil, nil)

	ctx.Set("k", "v")
	v, ok := ctx.Get("k")
	require.True(t, ok)
	require.Equal(t, "v", v)

	all := ctx.All()
	require.Equal(t, map[string]any{"k": "v"}, all)
}

func TestContext_Set_ReservedKeyForbidden_V2Sugar(t *testing.T) {
	ctx := NewContext(nil, nil)

	ctx.Set("_remilia_internal_x", 1)
	_, ok := ctx.Get("_remilia_internal_x")
	require.False(t, ok)
}

func TestContext_SetAll_ReturnsCopy(t *testing.T) {
	ctx := NewContext(nil, nil)
	ctx.Set("k", "v")

	all := ctx.All()
	all["k"] = "mutated"

	v, ok := ctx.Get("k")
	require.True(t, ok)
	require.Equal(t, "v", v)
}
