package middleware

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/require"
)

func TestDegradedExt_TypedFirst_ThenFallback(t *testing.T) {
	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	require.False(t, IsDegraded(ctx))

	// Fallback path: set user-state key only
	ctx.Set(CtxKeyDegraded, true)
	require.True(t, IsDegraded(ctx))

	// Typed path: set typed extension
	ctx2 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	SetDegraded(ctx2)
	require.True(t, IsDegraded(ctx2))
}
