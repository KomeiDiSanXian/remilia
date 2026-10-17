package engine

// candidate_meta_leak_test.go — 候选 Meta 不得跨候选泄漏的回归测试
//
// 回归背景：SetCandidateMeta(nil) 是空操作、引擎也只在 Meta 非 nil 时注入。
// 同一 Context 会依次服务多个候选，慢带里"带 Meta 的候选"（正则捕获组）之后
// 跟着"不带 Meta 的候选"（如第三方慢带索引）时，后者的 handler 会读到上一个
// 候选的正则捕获组——权限/路由判定可能因此基于错误的匹配结果。

import (
	"sync/atomic"
	"testing"

	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_CandidateMetaDoesNotLeakBetweenCandidates(t *testing.T) {
	et := string(platform.EventKindPrivateMessage)

	var stub *Matcher
	// 慢带自定义索引：排在正则索引之后执行，候选不携带 Meta
	idx := sliceIndex{
		band: BandSlow,
		get: func() ([]*Matcher, []*Matcher) {
			if stub == nil {
				return nil, nil
			}
			return []*Matcher{stub}, nil
		},
	}
	eng := newEngineForTest(t, WithMatcherIndex(idx))

	var regexRan, stubSawStaleMeta atomic.Bool

	// 携带 Meta 的正则候选（慢带）
	re := eng.On(et)
	re.Regex(`hello (\w+)`)
	re.SetPriority(10)
	re.Handle(func(c *ctx.Context) error {
		if _, ok := c.RegexResult(); ok {
			regexRan.Store(true)
		}
		return nil
	})

	// 不带 Meta 的慢带候选：不得读到上一个候选的正则捕获组
	stub = eng.On(et)
	stub.SetPriority(20)
	stub.Handle(func(c *ctx.Context) error {
		if _, ok := c.RegexResult(); ok {
			stubSawStaleMeta.Store(true)
		}
		return nil
	})

	eng.ProcessEvent(ctx.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hello world"), nil))

	require.True(t, regexRan.Load(), "正则候选应携带捕获组")
	assert.False(t, stubSawStaleMeta.Load(), "无 Meta 的候选不应继承上一个候选的 Meta")
}
