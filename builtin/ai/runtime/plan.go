// plan.go — 计划在回合编排里的使用约定：进度签名与重规划指令。
//
// 计划的数据结构与会话存取见 builtin/ai/session；两个内置计划动作
// （create_plan / update_plan_step）的形状与参数校验见 builtin/ai/catalog。
// 本文件只承载"计划如何被编排使用"：无进度检测的签名与步骤失败后的重规划指令。
// 计划端口（catalog.PlanAccess）的注入由装配侧直接调用 catalog，本包不参与——
// 否则运行时机制会反向知道内置动作的存在。
package runtime

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
)

// replanPrefix 重规划指令的用户可见前缀。
//
// 生成（[BuildReplanMessage]）与判重（[LastUserIsReplan]）必须用同一个字面量：
// 两处各写一份时，改文案会让判重静默失效、重规划指令被重复追加。
const replanPrefix = "计划步骤"

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

// BuildReplanMessage 构建"步骤失败 → 系统级重规划"指令。
// 由回合编排在检测到前序终态的前沿失败步骤时自动追加，
// 要求模型重新规划剩余步骤（而非自行猜测下一步）。
func BuildReplanMessage(step *session.PlanStep) protocol.Message {
	return protocol.Message{
		Role: protocol.RoleUser,
		Content: fmt.Sprintf(
			replanPrefix+" `%s`（%s）已标记失败。请重新评估剩余步骤：可以调用 create_plan 调整计划（跳过/替换失败步骤），或直接告知用户无法完成并说明原因。不要继续执行基于旧计划的后续步骤。",
			step.ID, step.Description),
	}
}
