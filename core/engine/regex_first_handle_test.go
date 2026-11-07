package engine

// regex_first_handle_test.go — Regex matcher 首次绑定 Handler 后的索引纳入
//
// 回归背景：正则桶按 handler 过滤（无 handler 的 matcher 不消耗正则求值），
// 注册时还没有 Handler 的 regex matcher 会被过滤掉，首次 Handle 必须补进来。
// 此前补录走全量 UpdateMatcherIndex(nil)，逐条注册 N 个 regex matcher
// 会退化为 O(N²)；现在只重建该事件类型的桶。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/require"
)

// TestMatcher_RegexFirstHandleAddsToBucket 验证首次 Handle 后 matcher 进入
// 正则桶，且捕获组能通过 ctx.RegexResult() 读取。
func TestMatcher_RegexFirstHandleAddsToBucket(t *testing.T) {
	eng := newEngineForTest(t)
	et := string(platform.EventKindPrivateMessage)

	m := eng.On(et)
	m.Regex(`^hello (\w+)$`)
	m.Handle(func(c *context.Context) error {
		res, ok := c.RegexResult()
		require.True(t, ok)
		require.Equal(t, "world", res.Groups[1])
		return nil
	})

	// 首次 Handle 之后正则桶必须已包含该 matcher
	require.Len(t, eng.state.Load().regexIndex[et], 1)

	eng.ProcessEvent(context.NewContextFromEvent(
		newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "hello world"), nil))
}

// TestMatcher_RegexBucketOnlyPerEventType 验证只重建对应事件类型的桶，
// 其他事件类型的正则桶保持不变。
func TestMatcher_RegexBucketOnlyPerEventType(t *testing.T) {
	eng := newEngineForTest(t)

	other := eng.On("OTHER_EVENT")
	other.Regex(`^x$`)
	other.Handle(func(*context.Context) error { return nil })

	before := eng.state.Load().regexIndex["OTHER_EVENT"]
	require.Len(t, before, 1)

	m := eng.On("NEW_EVENT")
	m.Regex(`^y$`)
	m.Handle(func(*context.Context) error { return nil })

	after := eng.state.Load()
	require.Len(t, after.regexIndex["NEW_EVENT"], 1)
	require.Equal(t, before, after.regexIndex["OTHER_EVENT"],
		"其他事件类型的正则桶不应被重建/复制")
}
