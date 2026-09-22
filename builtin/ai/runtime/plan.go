// plan.go — 计划在回合编排里的使用约定：进度签名、端口注入与重规划指令。
//
// 计划的数据结构与会话存取见 builtin/ai/session；两个内置计划动作
// （create_plan / update_plan_step）的形状与参数校验见 builtin/ai/catalog。
// 本文件只承载"计划如何被编排使用"：无进度检测的签名、供动作 Execute 取计划
// 端口的上下文传递，以及步骤失败后的重规划指令。
package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
)

// PlanSignature 生成计划进度签名（无进度检测：连续两轮签名不变则停止自动推进）。
func PlanSignature(p *session.Plan) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(p.Task)
	for _, s := range p.Steps {
		b.WriteString("|")
		b.WriteString(s.ID)
		b.WriteString("=")
		b.WriteString(string(s.Status))
	}
	return b.String()
}

// WithPlanSession 将会话注入 context，供 create_plan/update_plan_step 的 Execute 使用。
//
// 会话本身就是计划端口的实现（方法名一致），因此这里只做注入位置的转发。
func WithPlanSession(ctx context.Context, sess *session.Session) context.Context {
	return catalog.WithPlanAccess(ctx, sess)
}

// BuildReplanMessage 构建"步骤失败 → 系统级重规划"指令。
// 由回合编排在检测到前序终态的前沿失败步骤时自动追加，
// 要求模型重新规划剩余步骤（而非自行猜测下一步）。
func BuildReplanMessage(step *session.PlanStep) protocol.Message {
	return protocol.Message{
		Role: protocol.RoleUser,
		Content: fmt.Sprintf(
			"计划步骤 `%s`（%s）已标记失败。请重新评估剩余步骤：可以调用 create_plan 调整计划（跳过/替换失败步骤），或直接告知用户无法完成并说明原因。不要继续执行基于旧计划的后续步骤。",
			step.ID, step.Description),
	}
}
