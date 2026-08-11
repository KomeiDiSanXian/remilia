package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalAction(t *testing.T) {
	// 按钮 ID 解析
	approve, ignore, id := approvalAction("ai:approve:A1")
	assert.True(t, approve)
	assert.False(t, ignore)
	assert.Equal(t, "A1", id)

	approve, ignore, id = approvalAction("ai:deny:A2")
	assert.False(t, approve)
	assert.False(t, ignore)
	assert.Equal(t, "A2", id)

	// 无关按钮
	_, ignore, id = approvalAction("about:help")
	assert.True(t, ignore)
	assert.Equal(t, "", id)
}

func TestApproveDenyText(t *testing.T) {
	tests := []struct {
		in      string
		approve bool
		id      string
		ok      bool
	}{
		{"approve A1", true, "A1", true},
		{"批准 A1", true, "A1", true},
		{"允许:A1", true, "A1", true},
		{"同意 A1", true, "A1", true},
		{"deny A2", false, "A2", true},
		{"拒绝 A2", false, "A2", true},
		{"驳回：A2", false, "A2", true},
		{"随便聊聊", false, "", false},
		{"", false, "", false},
	}
	for _, tc := range tests {
		approve, id, ok := approveDenyText(tc.in)
		assert.Equal(t, tc.approve, approve, "approve for %q", tc.in)
		assert.Equal(t, tc.id, id, "id for %q", tc.in)
		assert.Equal(t, tc.ok, ok, "ok for %q", tc.in)
	}
}

func TestApprovalManagerLifecycle(t *testing.T) {
	m := newApprovalManager()

	// 注册并生成递增 ID
	r1 := &approvalRequest{RequesterID: "u1", result: make(chan bool, 1), createdAt: time.Now()}
	r2 := &approvalRequest{RequesterID: "u2", result: make(chan bool, 1), createdAt: time.Now()}
	m.register(r1)
	m.register(r2)
	assert.Equal(t, "A1", r1.ID)
	assert.Equal(t, "A2", r2.ID)

	// 发起者可审批
	assert.True(t, m.resolve("A1", "u1", true))
	assert.Equal(t, true, <-r1.result)

	// 非发起者不可审批
	assert.False(t, m.resolve("A2", "u3", true))
	assert.Len(t, r2.result, 0)

	// 发起者后续可审批
	assert.True(t, m.resolve("A2", "u2", false))
	assert.Equal(t, false, <-r2.result)

	// 已处理请求不可重复审批
	assert.False(t, m.resolve("A1", "u1", true))

	// 超时清理：过期请求关闭通道（等待方收到关闭 → 拒绝）
	r3 := &approvalRequest{RequesterID: "u1", result: make(chan bool, 1), createdAt: time.Now().Add(-2 * time.Minute)}
	m.register(r3)
	m.cleanupExpired(time.Minute, time.Now())
	select {
	case v, ok := <-r3.result:
		assert.False(t, ok, "expired request channel should be closed")
		assert.False(t, v)
	default:
		t.Fatal("expired request channel should be closed (waiting side sees rejection)")
	}
}

func TestNeedsApproval(t *testing.T) {
	p := &Plugin{}
	safe := Tool{Name: "safe"}
	sensitive := Tool{Name: "sensitive", RequiresApproval: true}

	assert.False(t, p.needsApproval(safe, "off"))
	assert.False(t, p.needsApproval(sensitive, "off"))
	assert.False(t, p.needsApproval(safe, "restricted"))
	assert.True(t, p.needsApproval(sensitive, "restricted"))
	assert.True(t, p.needsApproval(safe, "always"))
	assert.True(t, p.needsApproval(sensitive, "always"))
}

func TestSummarizeArgs(t *testing.T) {
	assert.Equal(t, "", summarizeArgs(nil))
	assert.Equal(t, "a=1, b=hello", summarizeArgs(map[string]any{"a": 1, "b": "hello"}))
	// 过滤 arguments 键（真实命令参数串）
	assert.Equal(t, "a=1", summarizeArgs(map[string]any{"a": 1, "arguments": "long raw args"}))
	// 长值截断（"k=" 前缀 + 40 截断 + "..."）
	long := make([]byte, 60)
	for i := range long {
		long[i] = 'x'
	}
	out := summarizeArgs(map[string]any{"k": string(long)})
	assert.Equal(t, 2+40+len("..."), len(out))
}

func TestFormatApprovalTimeout(t *testing.T) {
	assert.Equal(t, "30 秒", formatApprovalTimeout(30*time.Second))
	assert.Equal(t, "1 分钟", formatApprovalTimeout(time.Minute))
	assert.Equal(t, "2 分钟", formatApprovalTimeout(2*time.Minute))
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
		approvals: newApprovalManager(),
		cfg:       &Config{TriggerCmd: "/ai"},
	}

	done := make(chan bool, 1)
	go func() {
		done <- p.requestApproval(ctx, "sensitive_tool", "arg=1", 5*time.Second)
	}()

	// 等待审批请求注册
	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.approvals.mu.Lock()
		if len(p.approvals.pending) > 0 {
			for k := range p.approvals.pending {
				id = k
				break
			}
		}
		p.approvals.mu.Unlock()
		if id != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, id, "approval request should be registered")

	// 非发起者拒绝
	assert.False(t, p.approvals.resolve(id, "other_user", true))
	// 发起者批准
	assert.True(t, p.approvals.resolve(id, "u1", true))

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
		approvals: newApprovalManager(),
		cfg:       &Config{TriggerCmd: "/ai"},
	}
	start := time.Now()
	approved := p.requestApproval(ctx, "tool", "", 200*time.Millisecond)
	assert.False(t, approved, "timeout should be treated as rejection")
	assert.True(t, time.Since(start) >= 150*time.Millisecond, "should wait for timeout")
}

// TestApprovalModeFor 验证审批模式判定（全局 + per-group 覆盖）。
func TestApprovalModeFor(t *testing.T) {
	ctx, _ := newApprovalTestContext("u1")
	reg := NewToolRegistry()
	reg.Register(Tool{Name: "safe_tool"})
	reg.Register(Tool{Name: "sensitive_tool", RequiresApproval: true})

	// 全局 off：都不审批
	p := &Plugin{reg: reg, cfg: &Config{ToolApproval: "off"}}
	assert.False(t, p.approvalModeFor(ctx, "safe_tool"))
	assert.False(t, p.approvalModeFor(ctx, "sensitive_tool"))

	// 全局 restricted：仅审批标记工具
	p.cfg.ToolApproval = "restricted"
	assert.False(t, p.approvalModeFor(ctx, "safe_tool"))
	assert.True(t, p.approvalModeFor(ctx, "sensitive_tool"))

	// 全局 always：全部审批
	p.cfg.ToolApproval = "always"
	assert.True(t, p.approvalModeFor(ctx, "safe_tool"))
	assert.True(t, p.approvalModeFor(ctx, "sensitive_tool"))

	// per-group 覆盖：群策略 approval=off 覆盖全局 always
	policy := &GroupPolicy{}
	off := string(ApprovalOff)
	policy.Approval = &off
	gpm := newGroupPolicyManager(nil, "")
	gpm.SetGroup("group_1", policy)
	p.groupPolicies = gpm
	p.cfg.ToolApproval = "always"
	assert.False(t, p.approvalModeFor(ctx, "sensitive_tool"), "group policy off should override global always")

	// per-group 覆盖：群策略 approval=always 覆盖全局 off
	always := string(ApprovalAlways)
	policy2 := &GroupPolicy{Approval: &always}
	gpm2 := newGroupPolicyManager(nil, "")
	gpm2.SetGroup("group_1", policy2)
	p2 := &Plugin{reg: reg, cfg: &Config{ToolApproval: "off"}, groupPolicies: gpm2}
	assert.True(t, p2.approvalModeFor(ctx, "safe_tool"), "group policy always should override global off")
}

// TestHandleApprovalCommand 验证 /ai approve|deny 文本命令全链路。
func TestHandleApprovalCommand(t *testing.T) {
	sender := &approvalCtxSender{}
	p := &Plugin{
		approvals: newApprovalManager(),
		cfg:       &Config{TriggerCmd: "/ai"},
	}

	// 先注册一条待审批请求（发起者 u1）
	req := &approvalRequest{RequesterID: "u1", result: make(chan bool, 1), createdAt: time.Now()}
	p.approvals.register(req)
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
	case v := <-req.result:
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
	req2 := &approvalRequest{RequesterID: "u1", result: make(chan bool, 1), createdAt: time.Now()}
	p.approvals.register(req2)
	evt4 := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		"approve "+req2.ID,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u2"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	ctx4 := eventctx.NewContextFromEvent(evt4, sender)
	require.NoError(t, p.handleApprovalCommand(ctx4, true))
	assert.Len(t, req2.result, 0, "non-requester should not resolve approval")
}
