package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
)

func TestUserIDExt_Priority(t *testing.T) {
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	// When nothing is set, should fall back to event (which is empty here)
	require.Equal(t, "", GetUserID(ctx))

	// Typed extension should override event / empty.
	ctx.SetUserID("typed")
	require.Equal(t, "typed", GetUserID(ctx))
}
