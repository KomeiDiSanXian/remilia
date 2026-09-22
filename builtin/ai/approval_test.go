package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarizeArgs(t *testing.T) {
	assert.Equal(t, "", runtime.SummarizeArgs(nil))
	assert.Equal(t, "a=1, b=hello", runtime.SummarizeArgs(map[string]any{"a": 1, "b": "hello"}))
	// 过滤 arguments 键（真实命令参数串）
	assert.Equal(t, "a=1", runtime.SummarizeArgs(map[string]any{"a": 1, "arguments": "long raw args"}))
	// 长值截断（"k=" 前缀 + 40 截断 + "..."）
	long := make([]byte, 60)
	for i := range long {
		long[i] = 'x'
	}
	out := runtime.SummarizeArgs(map[string]any{"k": string(long)})
	assert.Equal(t, 2+40+len("..."), len(out))
}

// approvalCtxSender 记录发送的消息，返回模拟 SendResult。
type approvalCtxSender struct {
	mu      sync.Mutex
	replies []platform.OutboundMessage
}

func (s *approvalCtxSender) Send(_ context.Context, req platform.SendRequest) (platform.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replies = append(s.replies, req.Message)
	return platform.SendResult{MessageID: "mock-msg"}, nil
}

// newApprovalTestContext 构造带捕获 sender 的真实事件上下文。
func newApprovalTestContext(senderID string) (*eventctx.Context, *approvalCtxSender) {
	evt := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"hello",
		platform.WithSyntheticSender(platform.UserInfo{ID: senderID}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "group_1", IsGroup: true}),
	)
	sender := &approvalCtxSender{}
	ctx := eventctx.NewContextFromEvent(evt, sender)
	return ctx, sender
}

// TestRequestApproval_ResolveByTextCommand 验证审批全链路：
// 发起审批 → 按钮/文本命令 resolve 写入结果 → requestApproval 返回 true。
func TestRequestApproval_ResolveByTextCommand(t *testing.T) {
	ctx, _ := newApprovalTestContext("u1")
	p := &Plugin{
		approvals: execution.NewApprovalManager(),
		cfg:       &config.Config{TriggerCmd: "/ai"},
	}

	done := make(chan bool, 1)
	go func() {
		done <- p.requestApproval(ctx, "sensitive_tool", "arg=1", 5*time.Second)
	}()

	// 等待审批请求注册
	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ids := p.approvals.PendingIDs(); len(ids) > 0 {
			id = ids[0]
		}
		if id != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, id, "approval request should be registered")

	// 非发起者拒绝
	assert.False(t, p.approvals.Resolve(id, "other_user", true))
	// 发起者批准
	assert.True(t, p.approvals.Resolve(id, "u1", true))

	select {
	case approved := <-done:
		assert.True(t, approved, "requestApproval should return true after approval")
	case <-time.After(3 * time.Second):
		t.Fatal("requestApproval did not return after approval")
	}
}

// TestRequestApproval_Timeout 验证审批超时按拒绝处理。
func TestRequestApproval_Timeout(t *testing.T) {
	ctx, _ := newApprovalTestContext("u1")
	p := &Plugin{
		approvals: execution.NewApprovalManager(),
		cfg:       &config.Config{TriggerCmd: "/ai"},
	}
	start := time.Now()
	approved := p.requestApproval(ctx, "tool", "", 200*time.Millisecond)
	assert.False(t, approved, "timeout should be treated as rejection")
	assert.True(t, time.Since(start) >= 150*time.Millisecond, "should wait for timeout")
}

// TestHandleApprovalCommand 验证 /ai approve|deny 文本命令全链路。
func TestHandleApprovalCommand(t *testing.T) {
	sender := &approvalCtxSender{}
	p := &Plugin{
		approvals: execution.NewApprovalManager(),
		cfg:       &config.Config{TriggerCmd: "/ai"},
	}

	// 先注册一条待审批请求（发起者 u1）
	req := execution.NewApprovalRequest("u1", "", "", "")
	p.approvals.Register(req)
	id := req.ID

	// 发起者执行 /ai approve <ID> → 成功写入结果
	evt2 := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"approve "+id,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx2 := eventctx.NewContextFromEvent(evt2, sender)
	require.NoError(t, p.handleApprovalCommand(ctx2, true))
	select {
	case v := <-req.Result():
		assert.True(t, v, "approval should be true")
	default:
		t.Fatal("approval result should be written")
	}

	// 重复审批失败（已处理）
	evt3 := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"approve "+id,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx3 := eventctx.NewContextFromEvent(evt3, sender)
	require.NoError(t, p.handleApprovalCommand(ctx3, true)) // 不 panic，静默失败

	// 非发起者审批失败
	req2 := execution.NewApprovalRequest("u1", "", "", "")
	p.approvals.Register(req2)
	evt4 := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"approve "+req2.ID,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u2"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx4 := eventctx.NewContextFromEvent(evt4, sender)
	require.NoError(t, p.handleApprovalCommand(ctx4, true))
	assert.Len(t, req2.Result(), 0, "non-requester should not resolve approval")
}
