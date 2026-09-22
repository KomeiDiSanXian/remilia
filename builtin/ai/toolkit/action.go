// action.go — 动作的描述与策略：把"动作是什么"和"它该不该留下"拆开。
//
// 现状里这两个问题被同一个字段回答：Categories 既表示类别，又通过
// "空集视为 general" 隐式决定"是否必保"。于是每新增一个未标注类别的内置能力，
// 必保集就膨胀一项，ToolSelectMax 逐步失效。
//
// 现在显式拆成两层：
//   - ActionSpec   描述"这是什么"（名称、描述、参数、类别），给模型与检索用
//   - ActionPolicy 描述"选择与安全策略"（保留级别、审批、权限），给选择与闸门用
//
// 两者合成管线内部的动作类型 [Action]：选择、稳定、协议序列化与策略评估读的都是它，
// 执行载荷（Execute）只在注册表里按名称取用，"动作是什么"与"动作怎么执行"
// 因此在类型层面分开，而不是靠约定保证选择管线不碰执行逻辑。
//
// 两者都由 Tool 保真派生（见 ActionSpecOf / ActionPolicyOf），派生是对外行为的
// 唯一事实来源：改动这里的映射就等于改动行为，因此必须有测试守护。
package toolkit

import (
	"slices"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
)

// SelectionClass 动作在工具选择中的保留级别。
//
// 它只回答"当前选择策略是什么"，不回答"这是什么类别"——后者是 ActionSpec 的类别。
type SelectionClass uint8

const (
	// SelectionOptional 正常选择阶段可被淘汰：按相关度打分竞争，未命中即落选。
	SelectionOptional SelectionClass = iota
	// SelectionBaseline 默认保留：正常情况下进入稳定工具集，
	// 且在"本轮无需主动动作"时仍必须保留（见 KeepsWhenNoAction）。
	SelectionBaseline
	// SelectionMandatory 永远保留：任何选择策略下都不得被淘汰。
	SelectionMandatory
)

// String 返回保留级别的可读名称（用于日志与测试断言）。
func (c SelectionClass) String() string {
	switch c {
	case SelectionOptional:
		return "optional"
	case SelectionBaseline:
		return "baseline"
	case SelectionMandatory:
		return "mandatory"
	default:
		return "unknown"
	}
}

// KeepsWhenNoAction 报告该保留级别在"本轮无需主动动作"时是否仍必须保留。
//
// 只有 Optional 会被抑制；Baseline 与 Mandatory 都必须保留，
// 否则工具段会在 [] 与 [...] 之间来回切换，击穿 History 之前的前缀缓存。
func (c SelectionClass) KeepsWhenNoAction() bool {
	return c == SelectionBaseline || c == SelectionMandatory
}

// ActionSpec 动作的模型可见描述（不含执行）。
//
// 这是"给模型看什么"与"给检索看什么"的唯一来源：协议层序列化与相关度打分
// 都从这里取值，而不是各自去读 Tool 的散落字段。
type ActionSpec struct {
	// Name 动作名称，模型通过它调用动作。
	Name string
	// Description 动作描述，模型据此决定是否调用。
	Description string
	// Parameters 参数的 JSON Schema 描述。
	Parameters protocol.ToolParamSchema
	// Categories 动作所属类别（空集表示未分类，等价于 general 类别）。
	Categories []string
}

// ActionPolicy 动作的选择与安全策略（不含执行）。
type ActionPolicy struct {
	// Selection 工具选择中的保留级别。
	Selection SelectionClass
	// RequiresApproval 是否需要用户审批（tool_approval=restricted 时生效）。
	RequiresApproval bool
	// AlwaysRequireApproval 是否在任意审批模式（含 off）下都强制审批。
	AlwaysRequireApproval bool
	// Permissions 执行所需的 RBAC 权限（任一命中即放行）。
	Permissions []string
}

// Action 管线内部的一个动作：模型可见描述 + 选择与安全策略，不含执行载荷。
//
// 候选发现、打分排序、稳定策略、协议序列化与策略评估读的都是它；执行载荷
// （Execute）只由执行入口按名称从注册表取用（见 [(*ToolRegistry).Get]）。
type Action struct {
	// Spec 模型可见描述。
	Spec ActionSpec
	// Policy 选择与安全策略。
	Policy ActionPolicy
}

// ActionOf 从工具保真派生出管线动作。
func ActionOf(t Tool) Action {
	return Action{Spec: ActionSpecOf(t), Policy: ActionPolicyOf(t)}
}

// ActionsOf 批量派生管线动作。
// 供仍持有工具的执行路径使用（如 Skill 子循环：工具要执行，动作要发给模型）。
func ActionsOf(tools []Tool) []Action {
	out := make([]Action, 0, len(tools))
	for _, t := range tools {
		out = append(out, ActionOf(t))
	}
	return out
}

// ActionSpecOf 从工具派生模型可见描述。
func ActionSpecOf(t Tool) ActionSpec {
	return ActionSpec{
		Name:        t.Name,
		Description: t.Description,
		Parameters:  t.Parameters,
		Categories:  t.Categories,
	}
}

// ActionPolicyOf 从工具派生选择与安全策略。
//
// 保留级别的映射是保真的：原有"通用工具"（Categories 为空或含 general）
// 一律映射为 [SelectionBaseline]，其余为 [SelectionOptional]，
// 因此工具集组成与重构前逐项一致。
func ActionPolicyOf(t Tool) ActionPolicy {
	return ActionPolicy{
		Selection:             SelectionClassOf(t),
		RequiresApproval:      t.RequiresApproval,
		AlwaysRequireApproval: t.AlwaysRequireApproval,
		Permissions:           t.Permissions,
	}
}

// SelectionClassOf 是"通用工具"这一既有概念与保留级别之间的翻译函数。
//
// 它是该映射的唯一事实来源：选择路径只认 [SelectionClass]，
// 而"空类别视为通用"的兼容语义只在这里定义一次。
func SelectionClassOf(t Tool) SelectionClass {
	if IsGeneralTool(t) {
		return SelectionBaseline
	}
	return SelectionOptional
}

// IsGeneralTool 判断工具是否属于通用集（Categories 为空或含 general）。
// 通用工具恒被选中，作为模型的基础兜底能力。
func IsGeneralTool(t Tool) bool {
	if len(t.Categories) == 0 {
		return true
	}
	return slices.Contains(t.Categories, CategoryGeneral)
}
