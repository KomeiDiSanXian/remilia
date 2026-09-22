// approval.go — 命令执行审批闸门：待审批请求的登记、应答与超时清理。
//
// 对齐官方 OpenClaw 插件的 Command Execution Approval 能力：当 AI 需要执行
// 敏感动作时，先向用户发送审批请求（按钮 + 文本命令双通道），用户允许后
// 才真正执行。
//
// 审批模式（配置 tool_approval）：
//   - "off"（默认）: 不审批，动作直接执行
//   - "restricted": 仅审批标记 RequiresApproval=true 的动作
//   - "always":    审批所有动作调用
//
// 交互双通道：
//   - 按钮: 平台支持回调按钮（CapButtons）时发送"✅ 允许 / ❌ 拒绝"按钮，
//     用户点击后经 EventKindInteraction 回调事件处理。
//   - 文本命令: 任何平台可用 /ai approve <ID> / /ai deny <ID> 或
//     自然语言"批准 <ID> / 拒绝 <ID>"。
//
// 安全约束：
//   - 只有**发起该动作调用的用户**才能审批（响应者校验见 ApprovalManager.Resolve）。
//   - 审批等待超时（approval_timeout，默认 60s）按拒绝处理，避免动作循环
//     无限挂起占用会话锁。
//
// 本文件只承载闸门自身的状态与判定。"把审批请求发给谁、等多久"读的是配置与
// 回复能力，属于装配侧，因此不在这里。

package execution

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ApprovalRequest 一条待审批的动作调用请求。
type ApprovalRequest struct {
	// ID 审批请求唯一标识（形如 "A1"、"A2"…，按会话递增）。
	ID string
	// ToolName 待审批的动作名。
	ToolName string
	// ArgsSummary 参数摘要（用于展示，不传递完整参数避免敏感信息泄漏）。
	ArgsSummary string
	// RequesterID 发起动作调用的用户 ID（只有该用户能审批）。
	RequesterID string
	// ChatID 会话 ID（群/私聊）。
	ChatID string
	// CreatedAt 创建时间（用于超时清理）。
	CreatedAt time.Time

	// result 审批结果通道：true=允许，false=拒绝。请求超时或被清理时关闭。
	result chan bool
	// done 标记请求已结束（防止重复响应）。
	done bool
}

// NewApprovalRequest 构造一条待审批请求（结果通道已就绪，CreatedAt 取当前时间）。
func NewApprovalRequest(requesterID, chatID, toolName, argsSummary string) *ApprovalRequest {
	return &ApprovalRequest{
		ToolName:    toolName,
		ArgsSummary: argsSummary,
		RequesterID: requesterID,
		ChatID:      chatID,
		result:      make(chan bool, 1),
		CreatedAt:   time.Now(),
	}
}

// Result 审批结果通道：true=允许，false=拒绝。请求超时或被清理时关闭，
// 等待方据此按拒绝处理。
func (r *ApprovalRequest) Result() <-chan bool { return r.result }

// ApprovalManager 管理全部待审批请求。
type ApprovalManager struct {
	mu      sync.Mutex
	seq     int
	pending map[string]*ApprovalRequest
}

// NewApprovalManager 创建空的审批管理器。
func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{pending: make(map[string]*ApprovalRequest)}
}

// nextID 生成下一个审批请求 ID（调用方须持有 m.mu 或单线程调用）。
func (m *ApprovalManager) nextID() string {
	m.seq++
	return fmt.Sprintf("A%d", m.seq)
}

// Register 注册一条待审批请求并写入其 ID。
func (m *ApprovalManager) Register(r *ApprovalRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ID = m.nextID()
	m.pending[r.ID] = r
}

// Resolve 处理一条审批响应（按钮或文本命令）。
// 校验响应者必须是请求发起者；请求不存在、已完成或响应者不符时返回 false。
func (m *ApprovalManager) Resolve(id, responderID string, approved bool) bool {
	m.mu.Lock()
	r, ok := m.pending[id]
	if !ok || r.done {
		m.mu.Unlock()
		return false
	}
	if r.RequesterID != "" && r.RequesterID != responderID {
		m.mu.Unlock()
		return false
	}
	r.done = true
	delete(m.pending, id)
	m.mu.Unlock()

	select {
	case r.result <- approved:
	default:
	}
	return true
}

// CleanupExpired 清理超时的待审批请求（关闭通道触发等待方超时路径）。
// 每次调用间隔至少 sweepInterval，避免高频加锁。
func (m *ApprovalManager) CleanupExpired(timeout time.Duration, now time.Time) {
	m.mu.Lock()
	var expired []*ApprovalRequest
	for id, r := range m.pending {
		if now.Sub(r.CreatedAt) > timeout {
			r.done = true
			delete(m.pending, id)
			expired = append(expired, r)
		}
	}
	m.mu.Unlock()
	for _, r := range expired {
		close(r.result) // 关闭通道 = 超时拒绝
	}
}

// PendingIDs 返回当前待审批请求的 ID（顺序不确定）。
func (m *ApprovalManager) PendingIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.pending))
	for id := range m.pending {
		ids = append(ids, id)
	}
	return ids
}

// PendingCount 返回当前待审批请求数量。
func (m *ApprovalManager) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// ApproveButtonPrefix / DenyButtonPrefix 是审批按钮的回调 ID 前缀。
// 按钮 ID 形如 "ai:approve:A1" / "ai:deny:A1"，由 EventKindInteraction
// 回调事件的内容（button_data）解析。
const (
	ApproveButtonPrefix = "ai:approve:"
	DenyButtonPrefix    = "ai:deny:"
)

// ApprovalAction 从按钮回调内容解析审批动作（approve/deny/ignore）。
func ApprovalAction(buttonData string) (approve, ignore bool, id string) {
	switch {
	case strings.HasPrefix(buttonData, ApproveButtonPrefix):
		return true, false, strings.TrimPrefix(buttonData, ApproveButtonPrefix)
	case strings.HasPrefix(buttonData, DenyButtonPrefix):
		return false, false, strings.TrimPrefix(buttonData, DenyButtonPrefix)
	default:
		return false, true, ""
	}
}

// ArgsNote 生成参数摘要展示文本。
func ArgsNote(summary string) string {
	if summary == "" {
		return ""
	}
	return "\n\n参数: `" + summary + "`"
}

// FormatApprovalTimeout 格式化审批超时展示文本。
func FormatApprovalTimeout(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	return fmt.Sprintf("%.0f 分钟", d.Minutes())
}

// ApproveDenyText 解析自然语言审批指令文本（如"批准 A1"/"拒绝 A1"）。
// 返回 (approve, id, ok)。
func ApproveDenyText(content string) (approve bool, id string, ok bool) {
	content = strings.TrimSpace(content)
	lower := strings.ToLower(content)
	for _, prefix := range []string{"approve", "允许", "批准", "同意"} {
		if strings.HasPrefix(lower, prefix) {
			id := strings.TrimSpace(content[len(prefix):])
			id = strings.TrimLeft(id, ":： \t")
			return true, id, id != ""
		}
	}
	for _, prefix := range []string{"deny", "拒绝", "驳回", "不同意"} {
		if strings.HasPrefix(lower, prefix) {
			id := strings.TrimSpace(content[len(prefix):])
			id = strings.TrimLeft(id, ":： \t")
			return false, id, id != ""
		}
	}
	return false, "", false
}
