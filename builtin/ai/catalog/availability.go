package catalog

// availability.go — 动作的可用性状态机。
//
// 动态来源（外部工具服务器等）会带来"来源暂时不可用"这类抖动。若把可用性
// 压成一个 bool，策略禁止、来源未加载、连接断开、被禁用、被移除会挤进同一
// 字段，最终只能靠补丁互相区分。因此这里把它显式建模为状态机：
//
//	状态                 目录是否保留   是否可选   说明
//	Ready                是             是         正常
//	UnavailablePolicy    是             否         策略禁止（RBAC/群策略），立即生效
//	UnavailableProvider  是             否         来源未加载 / 初始化失败
//	Disconnected         是             是         连接断开 / 重连中（软不可用）
//	Disabled             是             否         配置禁用（配置意图，可逆）
//	Removed              否             —          来源已确不含该动作（目录事实）
//
// 取舍：策略禁止与禁用是"权威不可用"，必须立即从可选集合移除（权限收缩立即
// 生效），但**不改变目录内容**（不产生目录代数推进）；断开/未加载是"软不可
// 用"，仍留在可选集合以避免工具段在 [] 与 [...] 之间抖动，只在真正调用时
// 由执行侧返回类型化错误。
//
// 目录内容（代数）只在成员增删时变化：见 [Availability.MembershipChanged]。

// Availability 动作在目录中的可用性状态。
type Availability uint8

const (
	// AvailabilityReady 正常可用。
	AvailabilityReady Availability = iota
	// AvailabilityPolicy 因策略（RBAC / 群策略）被禁止；权威不可用，立即生效。
	AvailabilityPolicy
	// AvailabilityProvider 来源未加载或初始化失败；软不可用。
	AvailabilityProvider
	// AvailabilityDisconnected 来源连接断开或重连中；软不可用。
	AvailabilityDisconnected
	// AvailabilityDisabled 被配置禁用；权威不可用（配置意图），可逆。
	AvailabilityDisabled
	// AvailabilityRemoved 来源已不再提供该动作；从目录移除。
	AvailabilityRemoved
)

// String 返回状态的可读名（用于日志与测试断言）。
func (a Availability) String() string {
	switch a {
	case AvailabilityReady:
		return "ready"
	case AvailabilityPolicy:
		return "unavailable_policy"
	case AvailabilityProvider:
		return "unavailable_provider"
	case AvailabilityDisconnected:
		return "disconnected"
	case AvailabilityDisabled:
		return "disabled"
	case AvailabilityRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

// InCatalog 报告该状态是否仍保留在目录中。只有"已移除"会离开目录。
func (a Availability) InCatalog() bool {
	return a != AvailabilityRemoved
}

// Selectable 报告该状态的动作是否仍可被选择暴露给模型。
//
// 软不可用（来源未加载/断开）仍可选：保留在集合里，调用时才失败，
// 以此避免工具段抖动击穿前缀缓存。策略禁止与禁用不可选（权威不可用）。
func (a Availability) Selectable() bool {
	switch a {
	case AvailabilityReady, AvailabilityDisconnected:
		return true
	default:
		return false
	}
}

// Soft 报告该状态是否为"软不可用"（保留可选、调用时报错）。
func (a Availability) Soft() bool {
	return a == AvailabilityProvider || a == AvailabilityDisconnected
}

// MembershipChanged 报告从 from 到 to 的迁移是否改变了目录成员关系。
// 成员关系变化才推进目录代数；软不可用切换、策略切换不推进。
func MembershipChanged(from, to Availability) bool {
	return from.InCatalog() != to.InCatalog()
}
