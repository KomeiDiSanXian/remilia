package execution

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApprovalAction(t *testing.T) {
	// 按钮 ID 解析
	approve, ignore, id := ApprovalAction("ai:approve:A1")
	assert.True(t, approve)
	assert.False(t, ignore)
	assert.Equal(t, "A1", id)

	approve, ignore, id = ApprovalAction("ai:deny:A2")
	assert.False(t, approve)
	assert.False(t, ignore)
	assert.Equal(t, "A2", id)

	// 无关按钮
	_, ignore, id = ApprovalAction("about:help")
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
		approve, id, ok := ApproveDenyText(tc.in)
		assert.Equal(t, tc.approve, approve, "approve for %q", tc.in)
		assert.Equal(t, tc.id, id, "id for %q", tc.in)
		assert.Equal(t, tc.ok, ok, "ok for %q", tc.in)
	}
}

func TestApprovalManagerLifecycle(t *testing.T) {
	m := NewApprovalManager()

	// 注册并生成递增 ID
	r1 := NewApprovalRequest("u1", "", "", "")
	r2 := NewApprovalRequest("u2", "", "", "")
	m.Register(r1)
	m.Register(r2)
	assert.Equal(t, "A1", r1.ID)
	assert.Equal(t, "A2", r2.ID)

	// 发起者可审批
	assert.True(t, m.Resolve("A1", "u1", true))
	assert.Equal(t, true, <-r1.Result())

	// 非发起者不可审批
	assert.False(t, m.Resolve("A2", "u3", true))
	assert.Len(t, r2.Result(), 0)

	// 发起者后续可审批
	assert.True(t, m.Resolve("A2", "u2", false))
	assert.Equal(t, false, <-r2.Result())

	// 已处理请求不可重复审批
	assert.False(t, m.Resolve("A1", "u1", true))

	// 超时清理：过期请求关闭通道（等待方收到关闭 → 拒绝）
	r3 := NewApprovalRequest("u1", "", "", "")
	r3.CreatedAt = time.Now().Add(-2 * time.Minute)
	m.Register(r3)
	m.CleanupExpired(time.Minute, time.Now())
	select {
	case v, ok := <-r3.Result():
		assert.False(t, ok, "expired request channel should be closed")
		assert.False(t, v)
	default:
		t.Fatal("expired request channel should be closed (waiting side sees rejection)")
	}
}

func TestFormatApprovalTimeout(t *testing.T) {
	assert.Equal(t, "30 秒", FormatApprovalTimeout(30*time.Second))
	assert.Equal(t, "1 分钟", FormatApprovalTimeout(time.Minute))
	assert.Equal(t, "2 分钟", FormatApprovalTimeout(2*time.Minute))
}
