// Package ai trim_boundary_test.go — 窗口裁剪的时机契约。
//
// 提示词前缀缓存按字节比对请求前缀，而裁剪会前移消息数组的头部：一次裁剪
// 会让"从裁剪点开始"的全部历史在下一请求里变成未命中。因此裁剪的成本不取决于
// 裁掉多少条，而取决于**裁剪发生的次数**。
//
// 历史实现把 TrimMessages 挂在两条高频路径上——每次 AppendMessageNoPersist
// （回合内每条 assistant/tool/反思/重规划）以及每次 SaveSession（含回合内
// 收尾落库）——于是最大的受害者恰是本回合刚生成的工具结果前缀：那些消息
// 刚刚进入请求，还没来得及被任何一次后续请求复用就被裁掉。
//
// 本文件的用例冻结新的时机约定：**一个回合只在收尾裁剪一次**，回合内的
// 全部请求共享同一份只会增长的稳定前缀。
package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingToolProvider 前 depth-1 轮返回工具调用、最后一轮返回正文，
// 同时把每一轮请求的消息列表抄一份供断言。
type recordingToolProvider struct {
	mu    sync.Mutex
	depth int
	calls int
	reqs  [][]protocol.Message
}

func (r *recordingToolProvider) Chat(context.Context, *protocol.ChatRequest) (*protocol.ChatResponse, error) {
	return &protocol.ChatResponse{Content: "done"}, nil
}

func (r *recordingToolProvider) ChatStream(_ context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
	r.mu.Lock()
	r.calls++
	round := r.calls
	r.reqs = append(r.reqs, append([]protocol.Message(nil), req.Messages...))
	r.mu.Unlock()

	ch := make(chan protocol.StreamEvent, 3)
	if round < r.depth {
		ch <- protocol.StreamEvent{Type: protocol.StreamEventToolCall,
			ToolCall: &protocol.ToolCall{ID: "c" + string(rune('0'+round)), Name: "noop"}}
	} else {
		ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "done"}
	}
	ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
	close(ch)
	return ch, nil
}

// toolTurnPlugin 组装一个"每轮都请求工具"的最小插件，便于在一次
// processWithTools 里产生多条 assistant/tool 中间消息。
func toolTurnPlugin(t *testing.T, maxHistory, maxDepth int) (*Plugin, *session.Session, *recordingToolProvider) {
	t.Helper()
	prov := &recordingToolProvider{depth: maxDepth}
	p := &Plugin{
		cfg: &config.Config{
			MaxDepth:     maxDepth,
			APITimeout:   5 * time.Second,
			ToolTimeout:  3 * time.Second,
			ToolParallel: 1,
		},
		sm:       session.NewSessionManager(100, maxHistory, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		prov:     prov,
	}
	p.reg.Register(toolkit.Tool{
		Name: "noop",
		Execute: func(context.Context, *toolkit.Invocation, map[string]any) (toolkit.ToolResult, error) {
			return toolkit.TextResult("ok"), nil
		},
	})
	return p, p.sm.GetOrCreate("trim:turn", "u", "c"), prov
}

// TestTrimDoesNotRunMidTurn 冻结核心约定：一个回合内的多轮请求共享同一份
// 只增不减的稳定前缀——裁剪不得发生在回合中途。
//
// 旧实现下（每次 AppendMessageNoPersist 都裁剪），maxHistory=6 的会话在
// 第 3 轮就会裁到窗口内，第 4 轮请求的前缀因此与第 3 轮大幅错位：本回合
// 刚生成的工具结果在一次请求之后就被丢弃，缓存复用为零。
func TestTrimDoesNotRunMidTurn(t *testing.T) {
	const (
		maxHistory = 6
		maxDepth   = 4
	)
	p, sess, prov := toolTurnPlugin(t, maxHistory, maxDepth)

	p.sm.AppendMessage(sess, protocol.Message{Role: protocol.RoleUser, Content: "开始"})
	require.True(t, sess.BeginTurn())
	defer sess.EndTurn()

	evt := platform.NewSyntheticEvent("c2c", "开始")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	_, err := p.processWithTools(ctx, sess)
	require.NoError(t, err)

	prov.mu.Lock()
	reqs := append([][]protocol.Message(nil), prov.reqs...)
	prov.mu.Unlock()
	require.Len(t, reqs, maxDepth, "每一轮都应发出一次请求")

	// 回合内第 n 轮的请求必须是第 n+1 轮请求的严格前缀：任何一次中途裁剪
	// 都会让这条性质立刻不成立。
	for i := 0; i+1 < len(reqs); i++ {
		shared := commonMessagePrefix(reqs[i], reqs[i+1])
		assert.Equal(t, len(reqs[i]), shared,
			"第 %d 轮请求必须是第 %d 轮请求的前缀（回合中途不得裁剪），公共前缀 %d/%d",
			i+1, i+2, shared, len(reqs[i]))
	}

	// 最后一轮请求里仍能看到第一轮产生的工具结果：本回合的中间消息没有被裁掉。
	last := reqs[len(reqs)-1]
	foundFirstTool := false
	for _, m := range last {
		if m.Role == protocol.RoleTool && m.ToolCallID == "c1" {
			foundFirstTool = true
			break
		}
	}
	assert.True(t, foundFirstTool, "回合中途产生的工具结果必须仍在本次请求里")

	// 裁剪只发生在回合收尾：session 里此刻可能已被 TrimWindow 收敛到窗口内。
	assert.LessOrEqual(t, len(sess.SnapshotMessages()), maxHistory,
		"回合收尾应把历史收敛到窗口内")
}

// TestTrimDoesNotRunOnTurnSavings 冻结落库路径同样不裁剪：回合内的
// SaveSession 不得顺带改动消息头部。
func TestTrimDoesNotRunOnTurnSavings(t *testing.T) {
	const maxHistory = 4
	p, sess, _ := toolTurnPlugin(t, maxHistory, 1)

	for i := range 20 {
		p.sm.AppendMessageNoPersist(sess, protocol.Message{Role: protocol.RoleUser, Content: "m" + string(rune('a'+i%26))})
	}
	require.Len(t, sess.SnapshotMessages(), 20)
	require.True(t, sess.BeginTurn())

	// 回合进行中落库：这是 processWithTools 的 defer 行为，不得裁剪。
	p.sm.SaveSession(sess)
	assert.Len(t, sess.SnapshotMessages(), 20, "回合进行中落库不得裁剪")

	sess.EndTurn()
	p.sm.SaveSession(sess) // 回合边界的落库
	assert.LessOrEqual(t, len(sess.SnapshotMessages()), maxHistory, "回合结束后的落库应裁剪")
}

// TestTrimWindowIsTheOnlyTurnBoundaryEntry 冻结裁剪入口：
// TrimWindow 无论回合是否活跃都执行（它是回合收尾的显式调用）。
func TestTrimWindowIsTheOnlyTurnBoundaryEntry(t *testing.T) {
	const maxHistory = 5
	sm := session.NewSessionManager(10, maxHistory, time.Hour, nil)
	s := sm.GetOrCreate("trim:entry", "u", "c")

	for range 30 {
		sm.AppendMessageNoPersist(s, protocol.Message{Role: protocol.RoleUser, Content: "m"})
	}
	require.Len(t, s.SnapshotMessages(), 30)

	sm.TrimWindow(s)
	got := s.SnapshotMessages()
	assert.LessOrEqual(t, len(got), maxHistory)
	require.NotEmpty(t, got)

	// System 消息优先保留的既有语义不变。
	s2 := &session.Session{Messages: []protocol.Message{
		{Role: protocol.RoleSystem, Content: "sys"},
		{Role: protocol.RoleUser, Content: "u1"},
		{Role: protocol.RoleAssistant, Content: "a1"},
		{Role: protocol.RoleUser, Content: "u2"},
	}}
	sm.TrimWindow(s2)
	assert.Equal(t, protocol.RoleSystem, s2.SnapshotMessages()[0].Role, "System 消息必须保留在首位")
}
