package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/internal/extensionimpl"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

// testExt is a test-only typed extension used to validate Clone copies extensions.
// It intentionally lives in this _test.go file.
// NOTE: treat extension value as immutable.
type testExt struct{ V string }

// internalOnlyExt is a test-only typed extension used to simulate framework-internal data.
type internalOnlyExt struct{ N int }

func TestContext_StateLayering_UserStateDoesNotSeeTypedExtensions(t *testing.T) {
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: []byte(`{"content":"/x"}`)}, nil)

	// user state
	ctx.Set("k", "v")
	val, ok := ctx.Get("k")
	require.True(t, ok)
	require.Equal(t, "v", val)

	// framework-internal typed extension should not be visible via ctx.Get/ctx.All
	ExtSet(ctx.Ext(), internalOnlyExt{N: 123})
	_, ok = ctx.Get("internal_test")
	require.False(t, ok)
	all := ctx.All()
	require.Equal(t, map[string]any{"k": "v"}, all)

	// but typed extension can be retrieved via ExtGet
	iex, ok := ExtGet[internalOnlyExt](ctx.Ext())
	require.True(t, ok)
	require.Equal(t, 123, iex.N)
}

func TestContext_Clone_CopiesUserStateAndExtensions(t *testing.T) {
	ctx := NewContext(&dto.Payload{Detail: []byte(`{"content":"/x"}`)}, nil)
	ctx.Set("u", "1")
	ExtSet(ctx.Ext(), testExt{V: "2"})
	ExtSet(ctx.Ext(), internalOnlyExt{N: 3})

	cloned := ctx.Clone()

	uv, ok := cloned.Get("u")
	require.True(t, ok)
	require.Equal(t, "1", uv)

	iv, ok := ExtGet[testExt](cloned.Ext())
	require.True(t, ok)
	require.Equal(t, "2", iv.V)

	ix, ok := ExtGet[internalOnlyExt](cloned.Ext())
	require.True(t, ok)
	require.Equal(t, 3, ix.N)
}

func TestContext_ParseCommand_CacheIsInternal(t *testing.T) {
	// message content
	detail, _ := sjson.SetBytes([]byte("{}"), "content", "/echo hello")
	ctx := NewContext(&dto.Payload{Detail: detail}, nil)

	args1, err := ctx.ParseCommand()
	require.NoError(t, err)
	require.NotNil(t, args1)

	// cache must not be visible to user state
	all := ctx.All()
	_, ok := all["_remilia_internal_command_args"]
	require.False(t, ok)

	// V2 cache should exist in typed extensions
	cache, ok := ExtGet[*extensionimpl.CommandArgsCacheV2](ctx.Ext())
	require.True(t, ok)
	require.NotNil(t, cache)
	require.Same(t, args1, cache.Args)

	args2, err := ctx.ParseCommand()
	require.NoError(t, err)
	// should reuse cache (same pointer)
	require.Same(t, args1, args2)
}

func TestContext_ParsedCommand_StoredInInternalState(t *testing.T) {
	ctx := NewContext(&dto.Payload{Detail: []byte(`{"content":"/x"}`)}, nil)
	pc := &command.Parsed{Raw: "/x"}
	ctx.SetParsedCommand(pc)

	// not visible to user state
	_, ok := ctx.Get("_remilia_internal_parsed_command")
	require.False(t, ok)

	got := ctx.GetParsedCommand()
	require.Same(t, pc, got)
}
