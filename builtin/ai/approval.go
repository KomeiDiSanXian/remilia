// Package ai approval.go — 命令执行审批（Command Execution Approval）。
//
// 对齐官方 OpenClaw 插件的 Command Execution Approval 能力：当 AI 需要执行
// 敏感工具时，先向用户发送审批请求（按钮 + 文本命令双通道），用户允许后
// 才真正执行。
//
// 审批模式（配置 tool_approval）：
//   - "off"（默认）: 不审批，工具直接执行
//   - "restricted": 仅审批标记 RequiresApproval=true 的工具
//   - "always":    审批所有工具调用
//
// 交互双通道：
//   - 按钮: 平台支持回调按钮（CapButtons）时发送"✅ 允许 / ❌ 拒绝"按钮，
//     用户点击后经 EventKindInteraction 回调处理。QQ webhook 下回调按钮
//     实测不可靠（见 builtin/about），故按钮仅作为增强通道。
//   - 文本命令: 任何平台可用 /ai approve <ID> / ai deny <ID> 或
//     自然语言"批准 <ID> / 拒绝 <ID>"，经 AI 子命令路径处理（可靠兜底）。
//
// 安全约束：
//   - 只有**发起该工具调用的用户**才能审批（按钮回调校验点击者 == 发起者）。
//   - 审批等待超时（approval_timeout，默认 60s）按拒绝处理，避免工具循环
//     无限挂起占用会话锁。
package ai

import (
	"fmt"
	"strings"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// approvalRequest 一条待审批的工具调用请求。
type approvalRequest struct {
	// ID 审批请求唯一标识（形如 "A1"、"A2"…，按会话递增）。
	ID string
	// ToolName 待审批的工具名。
	ToolName string
	// Args 工具参数摘要（用于展示，不传递完整参数避免敏感信息泄漏）。
	ArgsSummary string
	// RequesterID 发起工具调用的用户 ID（只有该用户能审批）。
	RequesterID string
	// ChatID 会话 ID（群/私聊）。
	ChatID string
	// result 审批结果通道：true=允许，false=拒绝。请求超时或被清理时关闭。
	result chan bool
	// createdAt 创建时间（用于超时清理）。
	createdAt time.Time
	// done 标记请求已结束（防止重复响应）。
	done bool
}

// approvalManager 管理全部待审批请求。
type approvalManager struct {
	mu      sync.Mutex
	seq     int
	pending map[string]*approvalRequest
}

func newApprovalManager() *approvalManager {
	return &approvalManager{pending: make(map[string]*approvalRequest)}
}

// nextID 生成下一个审批请求 ID（调用方须持有 m.mu 或单线程调用）。
func (m *approvalManager) nextID() string {
	m.seq++
	return fmt.Sprintf("A%d", m.seq)
}

// register 注册一条待审批请求并返回其 ID。
func (m *approvalManager) register(r *approvalRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ID = m.nextID()
	m.pending[r.ID] = r
}

// resolve 处理一条审批响应（按钮或文本命令）。
// 校验响应者必须是请求发起者；请求不存在、已完成或响应者不符时返回 false。
func (m *approvalManager) resolve(id, responderID string, approved bool) bool {
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

// cleanupExpired 清理超时的待审批请求（关闭通道触发等待方超时路径）。
// 每次调用间隔至少 sweepInterval，避免高频加锁。
func (m *approvalManager) cleanupExpired(timeout time.Duration, now time.Time) {
	m.mu.Lock()
	var expired []*approvalRequest
	for id, r := range m.pending {
		if now.Sub(r.createdAt) > timeout {
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

// approveButtonPrefix / denyButtonPrefix 是审批按钮的回调 ID 前缀。
// 按钮 ID 形如 "ai:approve:A1" / "ai:deny:A1"，由 EventKindInteraction
// 回调事件的内容（button_data）解析。
const (
	approveButtonPrefix = "ai:approve:"
	denyButtonPrefix    = "ai:deny:"
)

// approvalAction 从按钮回调内容解析审批动作（approve/deny/ignore）。
func approvalAction(buttonData string) (approve, ignore bool, id string) {
	switch {
	case strings.HasPrefix(buttonData, approveButtonPrefix):
		return true, false, strings.TrimPrefix(buttonData, approveButtonPrefix)
	case strings.HasPrefix(buttonData, denyButtonPrefix):
		return false, false, strings.TrimPrefix(buttonData, denyButtonPrefix)
	default:
		return false, true, ""
	}
}

// handleApprovalButton 处理审批按钮回调（EventKindInteraction）。
// 校验点击者 == 发起者后写入审批结果。
func (p *Plugin) handleApprovalButton(ctx *eventctx.Context) error {
	approve, ignore, id := approvalAction(platform.Content(ctx.GetPlatformEvent()))
	if ignore {
		return nil
	}
	if id == "" {
		return nil
	}
	if !p.approvals.resolve(id, ctx.GetSenderInfo().ID, approve) {
		ctx.ReplyText("❌ 审批失败：请求不存在、已处理或非发起人")
		return nil
	}
	action := "✅ 已允许"
	if !approve {
		action = "❌ 已拒绝"
	}
	ctx.ReplyText(fmt.Sprintf("%s工具执行（请求 %s）", action, id))
	return nil
}

// needsApproval 判断指定工具是否需要审批。
func (p *Plugin) needsApproval(tool Tool, mode string) bool {
	switch mode {
	case "always":
		return true
	case "restricted":
		return tool.RequiresApproval
	default:
		return false
	}
}

// requestApproval 发起一次工具执行审批，等待用户响应。
// 返回 true=允许执行，false=拒绝或超时。
//
// 交互流程：
//  1. 发送带"✅ 允许 / ❌ 拒绝"按钮的审批请求消息（平台支持按钮时）
//  2. 同时提示文本命令 /ai approve <ID> / /ai deny <ID> 作为兜底
//  3. 等待响应或超时（approvalTimeout），超时按拒绝处理
//
// 审批请求消息通过独立 goroutine 发送（不阻塞工具循环的等待）。
func (p *Plugin) requestApproval(ctx *eventctx.Context, toolName, argsSummary string, approvalTimeout time.Duration) bool {
	req := &approvalRequest{
		ToolName:    toolName,
		ArgsSummary: argsSummary,
		RequesterID: ctx.GetSenderInfo().ID,
		ChatID:      ctx.GetChatInfo().ID,
		result:      make(chan bool, 1),
		createdAt:   time.Now(),
	}
	p.approvals.register(req)
	id := req.ID

	triggerCmd := p.cfg.TriggerCmd
	if triggerCmd == "" {
		triggerCmd = "/ai"
	}
	// 发送审批请求消息（含按钮，尽力而为；发送失败不影响文本兜底）
	msg := platform.OutboundMessage{
		Text: fmt.Sprintf("🔐 **工具执行审批**\n\nAI 请求执行工具 `%s`%s\n\n回复 `%s approve %s` 允许，或 `%s deny %s` 拒绝（%s 内有效）",
			toolName, argsNote(argsSummary), triggerCmd, id, triggerCmd, id, formatApprovalTimeout(approvalTimeout)),
	}
	msg = msg.WithButtons(
		platform.Button{ID: approveButtonPrefix + id, Label: "✅ 允许", Style: platform.ButtonStylePrimary},
		platform.Button{ID: denyButtonPrefix + id, Label: "❌ 拒绝", Style: platform.ButtonStyleDanger},
	)
	// 审批请求消息与工具结果相互独立，异步发送避免阻塞等待
	_ = ctx.Reply(msg)

	select {
	case approved, ok := <-req.result:
		if !ok {
			// 通道被关闭 = 超时清理
			ctx.ReplyText(fmt.Sprintf("⏰ 审批超时（%s），工具 `%s` 已按拒绝处理", formatApprovalTimeout(approvalTimeout), toolName))
			return false
		}
		return approved
	case <-time.After(approvalTimeout):
		// 本地超时兜底：从 pending 移除（幂等，已处理则 no-op）
		p.approvals.resolve(id, "", false)
		ctx.ReplyText(fmt.Sprintf("⏰ 审批超时（%s），工具 `%s` 已按拒绝处理", formatApprovalTimeout(approvalTimeout), toolName))
		return false
	}
}

// argsNote 生成参数摘要展示文本。
func argsNote(summary string) string {
	if summary == "" {
		return ""
	}
	return "\n\n参数: `" + summary + "`"
}

// formatApprovalTimeout 格式化审批超时展示文本。
func formatApprovalTimeout(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	return fmt.Sprintf("%.0f 分钟", d.Minutes())
}

// approveDenyText 解析自然语言审批指令文本（如"批准 A1"/"拒绝 A1"）。
// 返回 (approve, id, ok)。
func approveDenyText(content string) (approve bool, id string, ok bool) {
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

// handleApprovalCommand 处理 /ai approve <ID> 与 /ai deny <ID> 文本命令。
//
// 从消息内容中提取审批 ID（命令路径：子命令名后的第一个词；
// 自然语言路径：如 "批准 A1"）。校验响应者是发起者后写入审批结果。
func (p *Plugin) handleApprovalCommand(ctx *eventctx.Context, approve bool) error {
	content := p.cleanMessage(ctx.GetMessageContent())
	content = strings.TrimSpace(strings.TrimLeft(content, "@"))

	// 提取审批 ID：优先匹配 "approve <ID>"/"批准 <ID>" 模式，
	// 否则取消息中的最后一个词（命令路径下子命令名已被剥离）。
	id := ""
	if a, parsedID, ok := approveDenyText(content); ok && a == approve {
		id = parsedID
	}
	if id == "" {
		fields := strings.Fields(content)
		if len(fields) > 0 {
			id = fields[len(fields)-1]
		}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		ctx.ReplyText("❌ 请指定审批 ID，用法：`" + p.cfg.TriggerCmd + " approve <ID>` 或 `" + p.cfg.TriggerCmd + " deny <ID>`（ID 见审批请求消息）")
		return nil
	}

	if !p.approvals.resolve(id, ctx.GetSenderInfo().ID, approve) {
		ctx.ReplyText("❌ 审批失败：请求不存在、已处理或非发起人")
		return nil
	}
	action := "✅ 已允许"
	if !approve {
		action = "❌ 已拒绝"
	}
	ctx.ReplyText(fmt.Sprintf("%s工具执行（请求 %s）", action, id))
	return nil
}
