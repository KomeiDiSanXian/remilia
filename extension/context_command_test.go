package extension

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/internal/extensionimpl"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
)

func TestParseCommand_Extension_WorksAndCaches(t *testing.T) {
	ctx := remilia.NewContext(&dto.Payload{Detail: []byte(`{"content":"/echo hello"}`)}, nil)

	args1, err := ParseCommand(ctx)
	require.NoError(t, err)
	require.NotNil(t, args1)
	require.Equal(t, "/echo", args1.Command)

	args2, err := WithCommand(ctx).ParseCommand()
	require.NoError(t, err)
	require.Same(t, args1, args2)

	// cache is internal, invisible to user state
	_, ok := ctx.All()["_remilia_internal_command_args"]
	require.False(t, ok)

	// V2 cache should exist
	_, ok = remilia.ExtGet[*extensionimpl.CommandArgsCacheV2](ctx.Ext())
	require.True(t, ok)
}
