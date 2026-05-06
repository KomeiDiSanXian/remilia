package context_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContextFromEvent(t *testing.T) {
	ctx := context.NewContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
	require.NotNil(t, ctx)

	assert.NotNil(t, ctx.GetPlatformEvent())
	assert.Equal(t, string(platform.EventKindPrivateMessage), ctx.GetEventType())

	ctx.Set("key1", "value1")
	val, ok := ctx.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func BenchmarkContextCreation(b *testing.B) {
	b.Run("New", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx := context.NewContextFromEvent(makeTestEvent("test", "PRIVATE_MESSAGE", "", platform.EventKindPrivateMessage), nil)
			_ = ctx
		}
	})
}
