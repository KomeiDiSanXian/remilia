package plugin

// manager_writer.go — 管理级别写视图接口
//
// ManagerWriter 是 Manager 对 Privileged 插件暴露的写视图，
// 通过 SetupContext.Admin 注入，仅当 PluginDescriptor.Privileged == true 时非 nil。
//
// 只读查询（插件列表、状态等）通过 SetupContext.Info（PluginInfo）访问。
// 管理类插件同时拥有 ctx.Info（只读）和 ctx.Admin（可写）两个视图。

// ManagerWriter 插件系统管理写视图，仅供声明了 Privileged: true 的插件使用。
//
// 只包含会影响系统运行状态的写操作。
// 只读查询请通过 ctx.Info（PluginInfo）访问。
//
// 示例（admin 插件）：
//
//	return &plugin.PluginDescriptor{
//	    Name:       "admin",
//	    Privileged: true,
//	    Setup: func(ctx *plugin.SetupContext) (any, error) {
//	        // 只读查询通过 ctx.Info
//	        plugins := ctx.Info.List()
//	        // 写操作通过 ctx.Admin
//	        if err := ctx.Admin.Reload("help"); err != nil { ... }
//	        return p, nil
//	    },
//	}
type ManagerWriter interface {
	// Reload 热重载指定插件
	Reload(name string) error

	// Disable 禁用插件（暂停 Matcher 分发，保留容器条目）
	Disable(name string) error

	// Enable 启用已禁用的插件
	Enable(name string) error

	// Unregister 注销插件（完全卸载）
	Unregister(name string) error

	// ForceUnregister 强制注销插件（忽略 Unload 错误，适用于 Error 状态的插件）
	ForceUnregister(name string) error
}

// managerWriterImpl 将 *Manager 包装为 ManagerWriter（仅暴露写操作）
type managerWriterImpl struct{ m *Manager }

func (w *managerWriterImpl) Reload(name string) error          { return w.m.Reload(name) }
func (w *managerWriterImpl) Disable(name string) error         { return w.m.Disable(name) }
func (w *managerWriterImpl) Enable(name string) error          { return w.m.Enable(name) }
func (w *managerWriterImpl) Unregister(name string) error      { return w.m.Unregister(name) }
func (w *managerWriterImpl) ForceUnregister(name string) error { return w.m.ForceUnregister(name) }

// newManagerWriter 创建 Manager 的写视图（内部使用）
func newManagerWriter(m *Manager) ManagerWriter {
	return &managerWriterImpl{m: m}
}
