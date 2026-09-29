package toolkit

// dynamic.go — 动态工具来源的通知契约。
//
// 大多数来源（内置动作、命令、显式注册的插件工具）在启动时一次性提供工具；
// 但外部工具服务器等来源的集合会运行时变化。实现本接口的来源在集合变化时
// 通知目录，由目录重新同步——来源本身不持有、也不修改目录状态。
type ToolChangeNotifier interface {
	// OnToolsChanged 注册回调；来源在自己的工具集合变化时调用它。
	// 回调应为幂等且轻量，目录侧会在回调中重新拉取当前工具集合。
	OnToolsChanged(fn func())
}
