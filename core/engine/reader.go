package engine

// reader.go — Engine 只读视图接口及包装器
//
// EngineReader 仅暴露查询类方法，不包含任何能修改 Engine 状态的操作。
// 供插件系统（plugin.PluginInfo.Coordinator()）使用。
//
// 关键设计：通过 engineReaderWrapper 包装 *Engine，
// 使类型断言 coord.(*engine.Engine) 在运行时返回 false，
// 从而在运行时层面也阻断写操作访问（而不仅依赖编译期接口检查）。

// EngineReader 是 *Engine 的只读视图。
//
// 实现此接口的类型只允许读取 Engine 的状态，
// 不能注册新 Matcher、删除 Matcher、修改中间件链或变更任何 Engine 配置。
type EngineReader interface {
	// --- 命令查询 ---

	// GetAllCommands 返回所有已注册命令的快照（只读）
	GetAllCommands() []CommandInfo

	// FindCommand 按名称查找命令，不存在时返回 nil
	FindCommand(name string) *CommandInfo

	// GetCommandsByPlugin 按插件名分组返回命令列表
	GetCommandsByPlugin() map[string][]CommandInfo

	// GetCommandsByCategory 按分类分组返回命令列表
	GetCommandsByCategory() map[string][]CommandInfo

	// --- Matcher 统计 ---

	// GetMatcherCount 返回当前注册的 Matcher 总数
	GetMatcherCount() int

	// GetMatcherStats 返回 Matcher 统计信息
	GetMatcherStats() MatcherStats

	// GetMaxMatchers 返回最大 Matcher 限制（0 表示无限制）
	GetMaxMatchers() int

	// GetTempMatcherCount 返回临时 Matcher 数量
	GetTempMatcherCount() int
}

// engineReaderWrapper 将 *Engine 包装为不可被断言穿透的只读视图。
//
// 由于 engineReaderWrapper 不是 *Engine，运行时类型断言
//
//	coord.(*Engine)
//
// 永远返回 false，从而在运行时层面也阻断写操作访问。
//
// 字段刻意不导出，防止通过反射绕过。
type engineReaderWrapper struct {
	e *Engine
}

// NewEngineReader 将 *Engine 包装为只读视图。
//
// 传入 nil 时返回 nil，调用方应自行判断。
func NewEngineReader(e *Engine) EngineReader {
	if e == nil {
		return nil
	}
	return &engineReaderWrapper{e: e}
}

// --- EngineReader 接口实现（委托给 *Engine）---

func (r *engineReaderWrapper) GetAllCommands() []CommandInfo        { return r.e.GetAllCommands() }
func (r *engineReaderWrapper) FindCommand(name string) *CommandInfo { return r.e.FindCommand(name) }
func (r *engineReaderWrapper) GetCommandsByPlugin() map[string][]CommandInfo {
	return r.e.GetCommandsByPlugin()
}
func (r *engineReaderWrapper) GetCommandsByCategory() map[string][]CommandInfo {
	return r.e.GetCommandsByCategory()
}
func (r *engineReaderWrapper) GetMatcherCount() int          { return r.e.GetMatcherCount() }
func (r *engineReaderWrapper) GetMatcherStats() MatcherStats { return r.e.GetMatcherStats() }
func (r *engineReaderWrapper) GetMaxMatchers() int           { return r.e.GetMaxMatchers() }
func (r *engineReaderWrapper) GetTempMatcherCount() int      { return r.e.GetTempMatcherCount() }

// 编译时断言：engineReaderWrapper 实现了 EngineReader
var _ EngineReader = (*engineReaderWrapper)(nil)
