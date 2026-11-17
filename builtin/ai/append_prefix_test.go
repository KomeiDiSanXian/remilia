// append_prefix_test.go — 只追加历史形状的前缀契约（本轮的核心收益）。
//
// 原生上下文通道（context_channel=auto）下，环境事实作为独立消息写在所属
// user 之前并随历史回放。这样第 N 轮的请求就是第 N+1 轮请求的**严格字节前缀**：
// 模型侧前缀缓存从"上一轮问答尾部必然失效"变成"整段可复用"。
//
// 这正是 docs/notes/33 里 append-only 形状要换来的东西，因此必须用断言锁住，
// 而不是只写在文档里。
package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appendPrefixPlugin 组装一个 auto 通道的最小插件，并记录每个请求的完整消息。
func appendPrefixPlugin(t *testing.T) (*Plugin, *session.Session, *eventctx.Context, func() [][]protocol.Message) {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs [][]protocol.Message
	)
	p := &Plugin{
		cfg: &config.Config{
			Provider:              "openai",
			BaseURL:               "https://api.openai.com/v1",
			IncludeRuntimeContext: true,
			ContextChannel:        config.ContextChannelAuto,
			ContextFields:         []string{"time", "platform", "weekday"},
			ContextTimezone:       "UTC",
			ContextLanguage:       "中文",
			SystemPrompt:          "你是助手",
			MaxDepth:              2,
			APITimeout:            5 * time.Second,
			ToolTimeout:           3 * time.Second,
		},
		sm:       session.NewSessionManager(120, 120, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov: &mockProvider{chatStreamFn: func(_ context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
			mu.Lock()
			reqs = append(reqs, append([]protocol.Message(nil), req.Messages...))
			mu.Unlock()
			ch := make(chan protocol.StreamEvent, 2)
			ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "答"}
			ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
			close(ch)
			return ch, nil
		}},
	}

	evt := platform.NewSyntheticEvent("c2c", "你好",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	ctx := eventctx.NewContextFromEvent(evt, nil)
	sess := p.sm.GetOrCreate("prefix:auto", "u1", "c1")

	return p, sess, ctx, func() [][]protocol.Message {
		mu.Lock()
		defer mu.Unlock()
		out := make([][]protocol.Message, len(reqs))
		copy(out, reqs)
		return out
	}
}

// runTurn 模拟 handler 的一轮：写 env → 写 user → 跑工具循环。
func runAutoTurn(t *testing.T, p *Plugin, sess *session.Session, ctx *eventctx.Context, question string) {
	t.Helper()
	runtime.SetSystemMessage(sess, p.buildStaticSystemPrompt(ctx))
	if env := p.envContextMessage(ctx, sess); env != nil {
		p.sm.AppendMessage(sess, *env)
	}
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: question})
	require.True(t, sess.BeginTurn())
	_, err := p.processWithTools(ctx, sess)
	sess.EndTurn()
	require.NoError(t, err)
}

// TestAppendOnlyHistoryKeepsTurnPrefix 冻结核心收益：第 N 轮请求是第 N+1 轮请求
// 的严格字节前缀。
//
// 改造前（user_suffix）：运行时上下文拼在 user 消息里，下一轮的同一 user 消息
// 字节已变，从它开始整段失配——"上一轮问答尾部永久不可复用"。
func TestAppendOnlyHistoryKeepsTurnPrefix(t *testing.T) {
	p, sess, ctx, requests := appendPrefixPlugin(t)

	runAutoTurn(t, p, sess, ctx, "第一问")
	runAutoTurn(t, p, sess, ctx, "第二问")
	runAutoTurn(t, p, sess, ctx, "第三问")

	reqs := requests()
	require.Len(t, reqs, 3)

	prefixOf := func(a, b []protocol.Message) int {
		n := min(len(a), len(b))
		for i := range n {
			aj, err := jsonx.Marshal(a[i])
			require.NoError(t, err)
			bj, err := jsonx.Marshal(b[i])
			require.NoError(t, err)
			if string(aj) != string(bj) {
				return i
			}
		}
		return n
	}

	for i := 0; i+1 < len(reqs); i++ {
		shared := prefixOf(reqs[i], reqs[i+1])
		assert.Equal(t, len(reqs[i]), shared,
			"第 %d 轮请求必须是第 %d 轮请求的字节前缀（append-only），公共前缀 %d/%d\nreq%d=%+v\nreq%d=%+v",
			i+1, i+2, shared, len(reqs[i]), i+1, reqs[i], i+2, reqs[i+1])
	}

	// 环境事实按内容去重：同一天内日期/星期/时区/语言都不变，因此只写一次，
	// 后续轮次直接复用历史里那一条（重复追加只会按回合线性占用窗口预算）。
	var envCount int
	for _, m := range reqs[len(reqs)-1] {
		if m.Role == protocol.RoleDeveloper {
			envCount++
		}
	}
	assert.Equal(t, 1, envCount, "同一份环境事实只应写一次并随历史复用")
}

// TestEnvContextDeduplicatesIdenticalFacts 冻结去重语义：
// 与历史里最近一条环境事实相同时不重复追加；不同时才追加一条新的。
//
// 为什么要去重：同一份事实（同一天、同语言、同时区）在多轮之间逐字节相同，
// 重复追加只会按回合线性占用窗口预算，并把每条 user 与自己的 env 切成碎片。
func TestEnvContextDeduplicatesIdenticalFacts(t *testing.T) {
	p, sess, ctx, _ := appendPrefixPlugin(t)

	countEnv := func() int {
		n := 0
		for _, m := range sess.SnapshotMessages() {
			if m.Role == protocol.RoleEnvContext {
				n++
			}
		}
		return n
	}

	first := p.envContextMessage(ctx, sess)
	require.NotNil(t, first, "首轮必须产生环境事实")
	p.sm.AppendMessage(sess, *first)
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "第一问"})
	require.Equal(t, 1, countEnv())

	// 同一份事实 → 不追加。
	assert.Nil(t, p.envContextMessage(ctx, sess), "同一份事实不得重复追加")
	assert.Equal(t, 1, countEnv())

	// 历史里最近一条是别的事实（模拟跨日后旧记录在前）→ 允许追加新的。
	otherDay := protocol.MarshalEnvFacts(protocol.EnvFacts{
		Date: "2020-01-01", Weekday: "星期三", Platform: "synthetic", Language: "中文",
	})
	p.sm.AppendMessage(sess, runtime.EnvContextMessage(otherDay))
	require.Equal(t, 2, countEnv())

	second := p.envContextMessage(ctx, sess)
	require.NotNil(t, second, "最近一条事实不同时必须写入一条新的")
	assert.Equal(t, first.Content, second.Content, "写入的仍是本轮事实")
	p.sm.AppendMessage(sess, *second)
	assert.Equal(t, 3, countEnv())

	// 再取一次：最近一条已与当前事实一致 → 继续去重。
	assert.Nil(t, p.envContextMessage(ctx, sess))
	assert.Equal(t, 3, countEnv())
}

// TestAppendOnlyAnthropicMovesEnvBehindUser 验证 Anthropic 形状下的位置约束：
// 每条环境事实都被挪到它所属 user 的**之后**，且仍是先行历史的稳定前缀。
func TestAppendOnlyAnthropicMovesEnvBehindUser(t *testing.T) {
	p, sess, ctx, requests := appendPrefixPlugin(t)
	p.cfg.Provider = "anthropic"
	p.cfg.BaseURL = "https://api.anthropic.com"
	// 探测记忆按端点索引，换端点后需要重新判定形状。
	p.contextProbe.Reset()

	runAutoTurn(t, p, sess, ctx, "第一问")
	runAutoTurn(t, p, sess, ctx, "第二问")

	reqs := requests()
	require.Len(t, reqs, 2)
	for _, req := range reqs {
		for i, m := range req {
			if m.Role != protocol.RoleSystemMid {
				continue
			}
			require.Greater(t, i, 0, "中途 system 不能是数组首条")
			assert.Equal(t, protocol.RoleUser, req[i-1].Role,
				"中途 system 必须紧跟 user 之后：%+v", req)
		}
	}

	// 第 1 轮仍是第 2 轮的字节前缀（只追加，不改写）。
	shared := commonMessagePrefix(reqs[0], reqs[1])
	assert.Equal(t, len(reqs[0]), shared, "Anthropic 形状同样必须只追加")
}

// TestAppendOnlyBudgetCountsEnvFacts 验证环境事实占用窗口预算：
// 加上事实不会悄悄突破 context_window。
func TestAppendOnlyBudgetCountsEnvFacts(t *testing.T) {
	p, sess, ctx, _ := appendPrefixPlugin(t)
	p.cfg.ContextWindow = 700 // 预留 512 输出余量后只剩很少

	runAutoTurn(t, p, sess, ctx, "题目")

	// 环境事实优先占用预算，因此必然出现；但总量不得超出窗口。
	msgs := sess.SnapshotMessages()
	var found bool
	for _, m := range msgs {
		if m.Role == protocol.RoleEnvContext {
			found = true
		}
	}
	assert.True(t, found, "环境事实应写入历史（受端点形状编码后才发出）")

	snap := p.buildDynamicContextSnapshot(ctx, sess)
	assert.LessOrEqual(t, snap.Tokens(), 700, "上下文用量不得超出窗口")
}
