package context

// candidate_meta_test.go — 候选 Meta 清除语义的回归测试
//
// 回归背景：SetCandidateMeta(nil) 是空操作，同一 Context 服务多个候选时，
// 后一个（无 Meta）候选的 handler 会读到前一个候选（正则捕获组）注入的 Meta。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetCandidateMeta_NilClears(t *testing.T) {
	c := newTestCtx()

	c.SetCandidateMeta(RegexMatch{Pattern: "^hello", Groups: []string{"hello"}})
	res, ok := c.RegexResult()
	require.True(t, ok)
	assert.Equal(t, "hello", res.Groups[0])

	// 无 Meta 的候选必须清除上一个候选留下的值
	c.SetCandidateMeta(nil)
	_, ok = c.RegexResult()
	assert.False(t, ok, "SetCandidateMeta(nil) 应清除已有 Meta")
	assert.Nil(t, c.CandidateMeta())

	// 再次注入仍生效
	c.SetCandidateMeta(RegexMatch{Pattern: "x", Groups: []string{"x"}})
	_, ok = c.RegexResult()
	assert.True(t, ok)
}

func TestExtensions_DeleteTyped(t *testing.T) {
	c := newTestCtx()
	c.SetCandidateMeta(RegexMatch{Pattern: "x"})
	require.NotNil(t, c.CandidateMeta())

	c.Ext().DeleteTyped[candidateMeta]()
	assert.Nil(t, c.CandidateMeta())
}
