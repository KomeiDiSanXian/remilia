// invocation.go — 一次工具调用的显式上下文。
//
// 历史包袱：工具签名 `Execute(ctx context.Context, args map[string]any)` 只有
// 标准库 context 一个通道，于是调用者身份、授权器、发送能力、目标解析、审计
// 来源、执行端口都被塞进 context（8 个槽位）。这些数据都是**按调用变化**的，
// 用显式参数传递更符合 Go 的惯例，也让"缺参数"在编译期暴露。
//
// Invocation 是这次改造的载体：工具从 `inv` 取能力，不再从 context 取。
// context 只保留取消/超时/追踪（以及跨 Provider 接口的观测标注）。

package toolkit

import (
	"maps"
	"reflect"
	"slices"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// Invocation 一次工具调用的全部调用期能力。
//
// 分层约定：toolkit 自身定义的调用期能力（调用者、授权器、发送/解析、来源、
// 技能链）用字段；catalog/session 定义的领域端口经 Invocation.SetPort /
// Invocation.Port 挂载，避免 toolkit 反向依赖上层包。工具应优先使用领域包
// 提供的 CapabilitiesFrom / PlansFrom 访问器，不要直接拼端口键。
//
// 约定：构造后不再修改（SkillPath 用 WithSkill 返回副本，端口 map 也会克隆）；
// 工具不得缓存它。所有字段都可能为 nil/零值——使用前按"能力是否可用"判断，
// 缺失时返回明确错误，而不是静默降级。
type Invocation struct {
	// Caller 调用者身份。
	Caller platform.UserInfo
	// Authorizer 授权器（RBAC + 审批策略）；nil 表示无法验证身份。
	Authorizer Authorizer
	// Sender 消息发送能力；nil 表示当前调用不可发送。
	Sender ToolSender
	// Resolver 目标解析能力（只读）；nil 表示不可解析。
	Resolver TargetResolver
	// Source 调用来源（审计用）。
	Source ToolSource
	// SkillPath 当前技能调用链（用于递归检测），最后一个是当前技能。
	SkillPath []string

	// ports 模块自有端口（如 catalog.Capabilities、catalog.PlanAccess）。
	// 按接口类型挂载与取回，避免 toolkit 反向依赖上层包。
	ports map[reflect.Type]any
}

// SetPort 把一个模块自有端口挂到本次调用，按端口接口类型索引。
//
// 以接口类型为键（而不是具体实现类型），取回时才能稳定命中；传 nil 视为
// 未注册。
func (inv *Invocation) SetPort[T any](v T) {
	if inv == nil || any(v) == nil {
		return
	}
	if inv.ports == nil {
		inv.ports = make(map[reflect.Type]any, 2)
	}
	inv.ports[reflect.TypeFor[T]()] = v
}

// Port 取回指定接口类型的端口；未挂载时返回零值与 false。
func (inv *Invocation) Port[T any]() (T, bool) {
	var zero T
	if inv == nil || inv.ports == nil {
		return zero, false
	}
	v, ok := inv.ports[reflect.TypeFor[T]()]
	if !ok {
		return zero, false
	}
	typed, ok := v.(T)
	return typed, ok
}

// CanUse 判断当前调用者能否使用该工具：优先用授权器；没有授权器时按安全
// 默认处理——声明了 Permissions 的工具一律不可用。
func (inv *Invocation) CanUse(t Tool) (bool, string) {
	if inv != nil && inv.Authorizer != nil {
		return inv.Authorizer.CanUse(t)
	}
	if len(t.Permissions) > 0 {
		return false, "当前调用没有授权上下文，无法使用需要权限的工具"
	}
	return true, ""
}

// WithSkill 返回追加了一层技能调用的副本（不修改原 Invocation）。
//
// ports 一并克隆：技能副本上新增端口不会泄漏到父调用或兄弟技能。
func (inv *Invocation) WithSkill(name string) *Invocation {
	if inv == nil {
		inv = &Invocation{}
	}
	clone := *inv
	clone.SkillPath = append(append([]string(nil), inv.SkillPath...), name)
	clone.ports = maps.Clone(inv.ports)
	return &clone
}

// CallerInfoFromInvocation 提取调用者身份；inv 为空时返回零值与 false。
// 工具应经它读取，而不是直接访问字段——自定义执行方可能不构造 Invocation。
func CallerInfoFromInvocation(inv *Invocation) (platform.UserInfo, bool) {
	if inv == nil {
		return platform.UserInfo{}, false
	}
	return inv.Caller, true
}

// AuthorizerFromInvocation 提取授权器；nil Invocation 视作无授权器。
func AuthorizerFromInvocation(inv *Invocation) (Authorizer, bool) {
	if inv == nil || inv.Authorizer == nil {
		return nil, false
	}
	return inv.Authorizer, true
}

// ToolSenderFromInvocation 提取消息发送能力；nil Invocation 视作不可发送。
func ToolSenderFromInvocation(inv *Invocation) (ToolSender, bool) {
	if inv == nil || inv.Sender == nil {
		return nil, false
	}
	return inv.Sender, true
}

// TargetResolverFromInvocation 提取目标解析能力。
func TargetResolverFromInvocation(inv *Invocation) (TargetResolver, bool) {
	if inv == nil || inv.Resolver == nil {
		return nil, false
	}
	return inv.Resolver, true
}

// ToolSourceFromInvocation 提取会话源信息；nil Invocation 时返回零值与 false。
func ToolSourceFromInvocation(inv *Invocation) (ToolSource, bool) {
	if inv == nil {
		return ToolSource{}, false
	}
	return inv.Source, true
}

// InSkill 判断指定技能是否已在调用链中（递归检测）。
func (inv *Invocation) InSkill(name string) bool {
	if inv == nil {
		return false
	}
	return slices.Contains(inv.SkillPath, name)
}
