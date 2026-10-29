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
	"context"
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
//  3. 等待响应、回合中断（interruptCtx）或超时（approvalTimeout），
//     超时与中断均按拒绝处理
//
// interruptCtx 是回合中断感知上下文（TurnCtx 派生）：/ai stop 或用户新消息
// 抢占会取消它——已停止的回合不能继续阻塞等待审批（同会话新消息会在
// 回合锁上排队，表现为"机器人无响应"），更不能在停止后因迟到的"允许"
// 执行副作用。interruptCtx 为 nil 时仅按超时/响应收尾（测试与后台路径）。
//
// 审批请求消息与工具结果相互独立，发送失败也不影响文本兜底。
func (p *Plugin) requestApproval(ctx *eventctx.Context, interruptCtx context.Context, toolName, argsSummary string, approvalTimeout time.Duration) (granted bool) {
	start := time.Now()
	outcome := "denied"
	defer func() { recordApproval(outcome, time.Since(start)) }()

	if interruptCtx == nil {
		interruptCtx = context.Background()
	}
	req := execution.NewApprovalRequest(ctx.GetSenderInfo().ID, ctx.GetChatInfo().ID, toolName, argsSummary)
	p.approvals.Register(req)
	id := req.ID

	triggerCmd := triggerCommand(p.cfg)
	// 发送审批请求消息（含按钮，尽力而为；发送失败不影响文本兜底）
	msg := platform.OutboundMessage{
		Text: fmt.Sprintf("🔐 **工具执行审批**\n\nAI 请求执行工具 `%s`%s\n\n回复 `%s approve %s` 允许，或 `%s deny %s` 拒绝（%s 内有效）",
			toolName, execution.ArgsNote(argsSummary), triggerCmd, id, triggerCmd, id, execution.FormatApprovalTimeout(approvalTimeout)),
	}
	msg = msg.WithButtons(
		platform.Button{ID: execution.ApproveButtonPrefix + id, Label: "✅ 允许", Style: platform.ButtonStylePrimary},
		platform.Button{ID: execution.DenyButtonPrefix + id, Label: "❌ 拒绝", Style: platform.ButtonStyleDanger},
	)
	// 审批请求消息与工具结果相互独立；发送失败不影响文本兜底。
	_ = ctx.Reply(msg)

	timer := time.NewTimer(approvalTimeout)
	defer timer.Stop()

	select {
	case approved, ok := <-req.Result():
		if !ok {
			// 通道被关闭 = 超时/停机清理
			outcome = "timeout"
			p.replyFormatted(ctx, fmt.Sprintf("⏰ 审批超时（%s），工具 `%s` 已按拒绝处理", execution.FormatApprovalTimeout(approvalTimeout), toolName))
			return false
		}
		if approved {
			outcome = "approved"
		} else {
			outcome = "denied"
		}
		return approved
	case <-timer.C:
		// 本地超时兜底：从 pending 移除并关闭结果通道（幂等，已处理则 no-op）。
		// 不能走 Resolve——它以发起者身份校验响应者，空响应者会被判为无权而
		// 静默失败，使请求永久残留在 pending 中。
		outcome = "timeout"
		p.approvals.Cancel(id)
		p.replyFormatted(ctx, fmt.Sprintf("⏰ 审批超时（%s），工具 `%s` 已按拒绝处理", execution.FormatApprovalTimeout(approvalTimeout), toolName))
		return false
	case <-interruptCtx.Done():
		// 回合被停止/抢占：取消审批并按拒绝处理，避免已停止的回合
		// 因迟到的"允许"执行副作用。
		outcome = "interrupted"
		p.approvals.Cancel(id)
		p.replyFormatted(ctx, fmt.Sprintf("🛑 回合已停止，工具 `%s` 的审批已取消", toolName))
		return false
	}
}

// handleApprovalCommand 处理 /ai approve <ID> 与 /ai deny <ID> 文本命令。
//
// 从消息内容中提取审批 ID（命令路径：子命令名后的第一个词；
// 自然语言路径：如 "批准 A1"）。校验响应者是发起者后写入审批结果。
func (p *Plugin) handleApprovalCommand(ctx *eventctx.Context, approve bool) error {
	content := runtime.CleanMessage(ctx.GetMessageContent(), p.triggerPrefix())
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
		p.replyFormatted(ctx, "❌ 请指定审批 ID，用法：`"+triggerCommand(p.cfg)+" approve <ID>` 或 `"+triggerCommand(p.cfg)+" deny <ID>`（ID 见审批请求消息）")
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
