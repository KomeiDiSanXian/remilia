package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestContext_StateLayering_UserStateDoesNotSeeInternal(t *testing.T) {
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: []byte(`{"content":"/x"}`)}, nil)

	// user state
	ctx.SetState("k", "v")
	val, ok := ctx.GetState("k")
	require.True(t, ok)
	require.Equal(t, "v", val)

	// internal state should not be visible via GetState
	ctx.internalSet("_remilia_internal_test", 123)
	_, ok = ctx.GetState("_remilia_internal_test")
	require.False(t, ok)

	// but internalGet can see it
	v2, ok := ctx.internalGet("_remilia_internal_test")
	require.True(t, ok)
	require.Equal(t, 123, v2)

	// GetAllState should only contain user state
	all := ctx.GetAllState()
	require.Equal(t, State{"k": "v"}, all)
}

func TestContext_Clone_CopiesUserAndInternalState(t *testing.T) {
	ctx := NewContext(&dto.Payload{Detail: []byte(`{"content":"/x"}`)}, nil)
	ctx.SetState("u", "1")
	ctx.internalSet("i", "2")

	cloned := ctx.Clone()

	uv, ok := cloned.GetState("u")
	require.True(t, ok)
	require.Equal(t, "1", uv)

	iv, ok := cloned.internalGet("i")
	require.True(t, ok)
	require.Equal(t, "2", iv)
}

func TestContext_ParseCommand_CacheIsInternal(t *testing.T) {
	// message content
	detail, _ := sjson.SetBytes([]byte("{}"), "content", "/echo hello")
	ctx := NewContext(&dto.Payload{Detail: detail}, nil)

	args1, err := ctx.ParseCommand()
	require.NoError(t, err)
	require.NotNil(t, args1)

	// cache must not be visible to user state
	_, ok := ctx.GetState("_remilia_internal_command_args")
	require.False(t, ok)

	// but internal should have it
	v, ok := ctx.internalGet("_remilia_internal_command_args")
	require.True(t, ok)
	require.NotNil(t, v)

	args2, err := ctx.ParseCommand()
	require.NoError(t, err)
	// should reuse cache (same pointer)
	require.Same(t, args1, args2)
}

func TestContext_ParsedCommand_StoredInInternalState(t *testing.T) {
	ctx := NewContext(&dto.Payload{Detail: []byte(`{"content":"/x"}`)}, nil)
	pc := &command.ParsedCommand{Raw: "/x"}
	ctx.SetParsedCommand(pc)

	// not visible to user state
	_, ok := ctx.GetState("_remilia_internal_parsed_command")
	require.False(t, ok)

	got := ctx.GetParsedCommand()
	require.Same(t, pc, got)
}
