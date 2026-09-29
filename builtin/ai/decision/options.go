// options.go — 选择与稳定策略的输入参数（由消费方声明）。
//
// 本包不依赖插件配置包（builtin/ai/config）：装配侧把配置里与本层相关的开关
// 显式映射成 [SelectionOptions]，签名因此直接暴露本层真正用到的参数。
package decision

import "time"

// 选项默认值（配置值非法时生效）。
const (
	// DefaultSelectMax 单轮发送给模型的工具数上限。
	DefaultSelectMax = 20
	// DefaultToolBudget 工具段的 token 预算。
	DefaultToolBudget = 8000
	// DefaultStickyMax 稳定集合中补充工具的数量上限。
	DefaultStickyMax = 8
)

// SelectionOptions 工具选择（SelectToolsForTurn）与会话级稳定策略
// （StabilizeToolSet）共用的输入参数。
type SelectionOptions struct {
	// Max 单轮发送给模型的工具数上限（tool_select_max；<=0 用 DefaultSelectMax）。
	Max int
	// Budget 工具段与补充项的 token 预算（tool_budget；<=0 用 DefaultToolBudget）。
	Budget int
	// Sticky 是否启用会话级稳定策略（tool_set_sticky）。
	Sticky bool
	// StickyMax 稳定集合中补充工具的数量上限（tool_set_sticky_max；<=0 用 DefaultStickyMax）。
	StickyMax int
	// StickyTTL 补充工具的空闲衰减窗口（tool_set_ttl；<=0 表示不衰减）。
	StickyTTL time.Duration
	// CatalogGeneration 当前目录代数（工具集合内容版本）。缓存仅在代数相同时
	// 复用：来源增删工具会推进代数，连接抖动（软不可用）不会。
	CatalogGeneration uint64
}

// maxTools 返回生效的工具数上限。
func (o SelectionOptions) maxTools() int {
	if o.Max <= 0 {
		return DefaultSelectMax
	}
	return o.Max
}

// toolBudget 返回生效的工具段 token 预算。
func (o SelectionOptions) toolBudget() int {
	if o.Budget <= 0 {
		return DefaultToolBudget
	}
	return o.Budget
}

// stickyMax 返回生效的补充工具数量上限。
func (o SelectionOptions) stickyMax() int {
	if o.StickyMax <= 0 {
		return DefaultStickyMax
	}
	return o.StickyMax
}

// stickyTTL 返回生效的补充工具空闲存活时长（<=0 表示不衰减）。
// 生产默认值来自 config.DefaultConfig（20 分钟）；配置负值（如 -1s）可关闭衰减。
func (o SelectionOptions) stickyTTL() time.Duration {
	if o.StickyTTL <= 0 {
		return 0
	}
	return o.StickyTTL
}
