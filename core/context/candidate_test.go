package context

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidateMeta_RoundTrip(t *testing.T) {
	ctx := &Context{}

	assert.Nil(t, ctx.CandidateMeta())

	ctx.SetCandidateMeta(RegexMatch{Pattern: `\d+`, Groups: []string{"42", "42"}})
	rm, ok := ctx.RegexResult()
	require.True(t, ok)
	assert.Equal(t, `\d+`, rm.Pattern)
	assert.Equal(t, []string{"42", "42"}, rm.Groups)
}

func TestCandidateMeta_NilMetaNoop(t *testing.T) {
	ctx := &Context{}
	ctx.SetCandidateMeta(nil)
	assert.Nil(t, ctx.CandidateMeta())

	_, ok := ctx.RegexResult()
	assert.False(t, ok, "未注入 RegexMatch 时 RegexResult 应返回 false")
}

func TestCandidateMeta_WrongType(t *testing.T) {
	ctx := &Context{}
	ctx.SetCandidateMeta("not a regex match")
	_, ok := ctx.RegexResult()
	assert.False(t, ok, "非 RegexMatch 类型时 RegexResult 应返回 false")
}
