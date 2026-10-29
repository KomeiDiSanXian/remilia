// mentions.go — 目标解析能力的适配器与 @ 提及语义说明。
//
// 解析规则本身在 builtin/ai/sendtool.go（内置别名、本会话近期发言者昵称、
// 已加入群群名、原始 ID 兜底）；动作层（builtin/ai/catalog）经
// [toolkit.TargetResolver] 端口消费该能力：
//
//   - 解析只读、不受审批门控——与 ToolSender.SendTo（需要审批的跨会话发送）
//     分开，避免"能解析"被误当成"能发送"；
//   - 适配器在工具 context 组装点注入（见 execute.go），send_message 的
//     @ 提及与 send_to 的 target 因此共用同一套规则；
//   - @ 提及的准入语义（ID 直通、昵称解析、歧义拒绝、群目标拒绝）在
//     catalog.resolveMentions。

package ai

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// targetResolverAdapter 把 ToolSender 的目标解析方法适配为 [toolkit.TargetResolver]。
// 只有 [*loopToolSender] 会走这个适配器（它携带插件的历史与群列表依赖）。
type targetResolverAdapter struct {
	sender toolkit.ToolSender
}

// Resolve 转发到发送器的 ResolveTarget，返回 (目标, 展示文本, 错误)。
func (a targetResolverAdapter) Resolve(ctx context.Context, raw string, isGroup bool) (toolkit.ChatTarget, string, error) {
	return a.sender.ResolveTarget(ctx, raw, isGroup)
}
