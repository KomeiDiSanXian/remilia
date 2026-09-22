// approval.go — 命令执行审批的交互入口。
//
// 审批闸门本身（待审批请求的登记、应答、超时清理，以及按钮 ID 与自然语言
// 指令的解析）已归位到 builtin/ai/execution（见 execution.ApprovalManager）。
// 本文件只保留与插件上下文耦合的部分：把审批请求发给用户并等待结果，
// 以及按钮回调与 /ai approve|deny 文本命令两个入口。
//
// 审批模式与交互双通道的完整说明见 execution/approval.go。
package ai

import (
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// handleApprovalButton 处理审批按钮回调（EventKindInteraction）。
// 校验点击者 == 发起者后写入审批结果。
func (e *executionState) handleApprovalButton(ctx *eventctx.Context) error {
	approve, ignore, id := execution.ApprovalAction(platform.Content(ctx.GetPlatformEvent()))
	if ignore {
		return nil
	}
	if id == "" {
		return nil
	}
	if !e.approvals.Resolve(id, ctx.GetSenderInfo().ID, approve) {
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
	req := execution.NewApprovalRequest(ctx.GetSenderInfo().ID, ctx.GetChatInfo().ID, toolName, argsSummary)
	p.approvals.Register(req)
	id := req.ID

	triggerCmd := p.cfg.TriggerCmd
	if triggerCmd == "" {
		triggerCmd = "/ai"
	}
	// 发送审批请求消息（含按钮，尽力而为；发送失败不影响文本兜底）
	msg := platform.OutboundMessage{
		Text: fmt.Sprintf("🔐 **工具执行审批**\n\nAI 请求执行工具 `%s`%s\n\n回复 `%s approve %s` 允许，或 `%s deny %s` 拒绝（%s 内有效）",
			toolName, execution.ArgsNote(argsSummary), triggerCmd, id, triggerCmd, id, execution.FormatApprovalTimeout(approvalTimeout)),
	}
	msg = msg.WithButtons(
		platform.Button{ID: execution.ApproveButtonPrefix + id, Label: "✅ 允许", Style: platform.ButtonStylePrimary},
		platform.Button{ID: execution.DenyButtonPrefix + id, Label: "❌ 拒绝", Style: platform.ButtonStyleDanger},
	)
	// 审批请求消息与工具结果相互独立，异步发送避免阻塞等待
	_ = ctx.Reply(msg)

	select {
	case approved, ok := <-req.Result():
		if !ok {
			// 通道被关闭 = 超时清理
			p.replyFormatted(ctx, fmt.Sprintf("⏰ 审批超时（%s），工具 `%s` 已按拒绝处理", execution.FormatApprovalTimeout(approvalTimeout), toolName))
			return false
		}
		return approved
	case <-time.After(approvalTimeout):
		// 本地超时兜底：从 pending 移除（幂等，已处理则 no-op）
		p.approvals.Resolve(id, "", false)
		p.replyFormatted(ctx, fmt.Sprintf("⏰ 审批超时（%s），工具 `%s` 已按拒绝处理", execution.FormatApprovalTimeout(approvalTimeout), toolName))
		return false
	}
}

// handleApprovalCommand 处理 /ai approve <ID> 与 /ai deny <ID> 文本命令。
//
// 从消息内容中提取审批 ID（命令路径：子命令名后的第一个词；
// 自然语言路径：如 "批准 A1"）。校验响应者是发起者后写入审批结果。
func (p *Plugin) handleApprovalCommand(ctx *eventctx.Context, approve bool) error {
	content := runtime.CleanMessage(ctx.GetMessageContent(), p.triggerCmd)
	content = strings.TrimSpace(strings.TrimLeft(content, "@"))

	// 提取审批 ID：优先匹配 "approve <ID>"/"批准 <ID>" 模式，
	// 否则取消息中的最后一个词（命令路径下子命令名已被剥离）。
	id := ""
	if a, parsedID, ok := execution.ApproveDenyText(content); ok && a == approve {
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
		p.replyFormatted(ctx, "❌ 请指定审批 ID，用法：`"+p.cfg.TriggerCmd+" approve <ID>` 或 `"+p.cfg.TriggerCmd+" deny <ID>`（ID 见审批请求消息）")
		return nil
	}

	if !p.approvals.Resolve(id, ctx.GetSenderInfo().ID, approve) {
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
