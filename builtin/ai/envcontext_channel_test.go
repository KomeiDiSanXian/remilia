// envcontext_channel_test.go — 原生上下文通道的装配契约。
//
// 这些用例冻结三件事：
//  1. 默认（user_suffix）不写历史、请求字节不变——升级不得击穿既有缓存；
//  2. 启用 auto 后，事实作为独立消息写在所属 user 之前，并逐轮随历史回放；
//  3. 端点拒绝原生角色时，本回合自动降级为 user 文本形态重试，且不影响预算轮次。
package ai

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// channelTestPlugin 组装一个最小插件；prov 由调用方提供。
func channelTestPlugin(prompt string, channel string, prov protocol.Provider) *Plugin {
	return &Plugin{
		cfg: &config.Config{
			Provider:              "openai",
			BaseURL:               "https://api.openai.com/v1",
			IncludeRuntimeContext: true,
			ContextChannel:        channel,
			ContextFields:         []string{"time", "platform"},
			ContextLanguage:       "中文",
			MaxDepth:              3,
			APITimeout:            5 * time.Second,
			ToolTimeout:           3 * time.Second,
			SystemPrompt:          prompt,
		},
		sm:       session.NewSessionManager(40, 40, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov:     prov,
	}
}

func channelTestContext() *eventctx.Context {
	evt := platform.NewSyntheticEvent("c2c", "你好",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	return eventctx.NewContextFromEvent(evt, nil)
}

// TestUserSuffixChannelKeepsHistoryClean 冻结默认行为：user_suffix 下不写
// RoleEnvContext，运行时上下文照旧拼进请求尾部。
func TestUserSuffixChannelKeepsHistoryClean(t *testing.T) {
	var (
		mu   sync.Mutex
		reqs [][]protocol.Message
	)
	p := channelTestPlugin("你是助手", config.ContextChannelUserSuffix,
		&mockProvider{chatStreamFn: func(_ context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
			mu.Lock()
			reqs = append(reqs, append([]protocol.Message(nil), req.Messages...))
			mu.Unlock()
			ch := make(chan protocol.StreamEvent, 2)
			ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "答"}
			ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
			close(ch)
			return ch, nil
		}})

	ctx := channelTestContext()
	sess := p.sm.GetOrCreate("ch:u1", "u1", "c1")
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "你好"})
	require.True(t, sess.BeginTurn())
	_, err := p.processWithTools(ctx, sess)
	sess.EndTurn()
	require.NoError(t, err)

	// 历史里不得出现 env 消息。
	for _, m := range sess.SnapshotMessages() {
		assert.NotEqual(t, protocol.RoleEnvContext, m.Role, "user_suffix 不得写环境事实消息")
	}

	// 运行时上下文仍出现在本轮 user 消息里（改造前的形状）。
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, reqs)
	last := reqs[len(reqs)-1]
	lastUser := -1
	for i, m := range last {
		if m.Role == protocol.RoleUser {
			lastUser = i
		}
	}
	require.GreaterOrEqual(t, lastUser, 0)
	assert.Contains(t, last[lastUser].Content, "运行时上下文")

	// 不得出现任何原生通道角色。
	for _, m := range last {
		assert.NotEqual(t, protocol.RoleLatestReminder, m.Role)
		assert.NotEqual(t, protocol.RoleDeveloper, m.Role)
		assert.NotEqual(t, protocol.RoleEnvContext, m.Role)
	}
}

// TestAutoChannelWritesEnvBeforeUser 冻结 auto 模式的历史形状：
// 环境事实作为独立消息写在 user 之前，并在请求里被编码成 developer。
func TestAutoChannelWritesEnvBeforeUser(t *testing.T) {
	var (
		mu   sync.Mutex
		reqs [][]protocol.Message
	)
	p := channelTestPlugin("你是助手", config.ContextChannelAuto,
		&mockProvider{chatStreamFn: func(_ context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
			mu.Lock()
			reqs = append(reqs, append([]protocol.Message(nil), req.Messages...))
			mu.Unlock()
			ch := make(chan protocol.StreamEvent, 2)
			ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "答"}
			ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
			close(ch)
			return ch, nil
		}})

	ctx := channelTestContext()
	sess := p.sm.GetOrCreate("ch:auto", "u1", "c1")
	runtime.SetSystemMessage(sess, p.buildStaticSystemPrompt(ctx))

	// 模拟 handler 的写入顺序：env 在 user 之前。
	env := p.envContextMessage(ctx, sess)
	require.NotNil(t, env, "auto 模式必须产生环境事实消息")
	p.sm.AppendMessage(sess, *env)
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "你好"})

	require.True(t, sess.BeginTurn())
	_, err := p.processWithTools(ctx, sess)
	sess.EndTurn()
	require.NoError(t, err)

	// 历史里保留中立存储角色（不绑定任何厂商协议）。
	msgs := sess.SnapshotMessages()
	require.GreaterOrEqual(t, len(msgs), 2)
	foundEnv := false
	for i, m := range msgs {
		if m.Role == protocol.RoleEnvContext {
			foundEnv = true
			require.Less(t, i+1, len(msgs))
			assert.Equal(t, protocol.RoleUser, msgs[i+1].Role, "env 必须紧邻其后的 user")
		}
	}
	assert.True(t, foundEnv, "auto 模式必须把环境事实写进历史")

	// 请求里被编码成 developer（OpenAI 兼容端点），且正文是可读指令块。
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, reqs)
	req := reqs[0]
	devIdx := -1
	for i, m := range req {
		if m.Role == protocol.RoleDeveloper {
			devIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, devIdx, 0, "请求里必须有 developer 消息: %+v", req)
	assert.Contains(t, req[devIdx].Content, "<environment>")
	assert.Contains(t, req[devIdx].Content, "平台: ")
	// 尾部不得再重复一份运行时上下文。
	for _, m := range req {
		if m.Role == protocol.RoleUser {
			assert.NotContains(t, m.Content, "===== 运行时上下文 =====",
				"原生通道下事实不得同时出现在尾部")
		}
	}
}

// TestAutoChannelFallsBackWhenRejected 冻结探测降级：端点以 400 拒绝原生角色时，
// 本回合自动改用 user 文本形态重试，且不消耗 max_depth 轮次。
func TestAutoChannelFallsBackWhenRejected(t *testing.T) {
	var (
		mu       sync.Mutex
		attempts int
		reqs     [][]protocol.Message
	)
	p := channelTestPlugin("你是助手", config.ContextChannelAuto,
		&mockProvider{chatStreamFn: func(_ context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
			mu.Lock()
			attempts++
			n := attempts
			reqs = append(reqs, append([]protocol.Message(nil), req.Messages...))
			mu.Unlock()
			if n == 1 {
				// 第一次：端点拒绝 developer 角色。
				return nil, &protocol.APIError{
					StatusCode: 400,
					Body:       `{"error":{"message":"invalid role: developer"}}`,
				}
			}
			ch := make(chan protocol.StreamEvent, 2)
			ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "答"}
			ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
			close(ch)
			return ch, nil
		}})

	ctx := channelTestContext()
	sess := p.sm.GetOrCreate("ch:fallback", "u1", "c1")
	env := p.envContextMessage(ctx, sess)
	require.NotNil(t, env)
	p.sm.AppendMessage(sess, *env)
	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "你好"})

	require.True(t, sess.BeginTurn())
	_, err := p.processWithTools(ctx, sess)
	sess.EndTurn()
	require.NoError(t, err, "降级后的重试应当成功")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, attempts, "首轮应重试一次")
	assert.True(t, p.contextProbe.IsBad(p.cfg.Provider, p.cfg.BaseURL), "端点应被标记为不支持")

	// 第一次请求有 developer，第二次（降级后）没有，且事实折进了 user 文本。
	first, second := reqs[0], reqs[1]
	hasDev := false
	for _, m := range first {
		if m.Role == protocol.RoleDeveloper {
			hasDev = true
		}
	}
	assert.True(t, hasDev, "探测请求应带原生通道")
	for _, m := range second {
		assert.NotEqual(t, protocol.RoleDeveloper, m.Role, "降级后不得再发原生角色")
	}
	merged := false
	for _, m := range second {
		if m.Role == protocol.RoleUser && strings.Contains(m.Content, "===== 运行时上下文 =====") {
			merged = true
		}
	}
	assert.True(t, merged, "降级后事实必须折进 user 文本")
}

// TestProbeRejectionDoesNotTriggerOnAuthError 冻结降级边界：鉴权失败不得让
// 环境事实永久回退。
func TestProbeRejectionDoesNotTriggerOnAuthError(t *testing.T) {
	p := channelTestPlugin("你是助手", config.ContextChannelAuto, &mockProvider{})
	authErr := &protocol.APIError{StatusCode: 401, Body: `{"error":{"message":"invalid api key"}}`}
	assert.False(t, p.tryDowngradeEnvChannel(authErr))
	assert.False(t, p.contextProbe.IsBad(p.cfg.Provider, p.cfg.BaseURL))

	// 上下文超长的 400 同样不应误判。
	ctxErr := &protocol.APIError{StatusCode: 400, Body: `context length exceeded`}
	assert.False(t, p.tryDowngradeEnvChannel(ctxErr))
	assert.False(t, p.contextProbe.IsBad(p.cfg.Provider, p.cfg.BaseURL))
}
