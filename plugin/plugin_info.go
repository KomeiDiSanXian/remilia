package plugin

import "github.com/KomeiDiSanXian/remilia/core/engine"

// Info 插件系统只读视图
//
// 通过 [SetupContext.Info] 访问，允许插件在 Setup 阶段查询其他插件的状态，
// 但不能执行任何写操作（注册/卸载/重载）。
type Info interface {
	// IsLoaded 检查指定插件是否已加载（Loaded 状态）
	IsLoaded(name string) bool

	// IsDisabled 检查指定插件是否被禁用
	IsDisabled(name string) bool

	// GetStatus 获取指定插件的运行状态，不存在时返回 nil
	GetStatus(name string) *Status

	// List 列出所有已注册插件的名称
	List() []string

	// Count 返回已注册插件的数量
	Count() int

	// GetMetadata 获取指定插件的元数据，不存在时返回 nil, false
	GetMetadata(name string) (*Metadata, bool)

	// ListWithMetadata 列出所有插件及其元数据
	ListWithMetadata() map[string]*Metadata

	// GetLoadOrder 返回插件加载顺序
	GetLoadOrder() []string

	// Get 获取指定插件的实例（用于查询运行时状态），不存在时返回 nil, false
	Get(name string) (*Instance, bool)

	// Coordinator 返回 Engine 的只读视图，供查询命令列表、Matcher 统计等只读操作。
	//
	// 返回 engine.Reader 而非 *engine.Engine，
	// 编译器强制阻止通过此接口调用任何写操作（On/RegisterCommand/DeleteMatcher 等）。
	Coordinator() engine.Reader
}

// managerInfoView 基于 *Manager 实现 Info 只读视图
type managerInfoView struct {
	m *Manager
}

func newPluginInfo(m *Manager) Info {
	if m == nil {
		return &nullPluginInfo{}
	}
	return &managerInfoView{m: m}
}

func (v *managerInfoView) IsLoaded(name string) bool              { return v.m.IsLoaded(name) }
func (v *managerInfoView) IsDisabled(name string) bool            { return v.m.IsDisabled(name) }
func (v *managerInfoView) List() []string                         { return v.m.List() }
func (v *managerInfoView) Count() int                             { return v.m.Count() }
func (v *managerInfoView) GetMetadata(n string) (*Metadata, bool) { return v.m.GetMetadata(n) }
func (v *managerInfoView) ListWithMetadata() map[string]*Metadata { return v.m.ListWithMetadata() }
func (v *managerInfoView) GetLoadOrder() []string                 { return v.m.GetLoadOrder() }
func (v *managerInfoView) Get(name string) (*Instance, bool)      { return v.m.Get(name) }

// Coordinator 返回包装后的只读视图，防止通过类型断言绕过只读限制。
//
// PluginCoordinator 已嵌入 Reader，可直接作为只读视图返回。
// 与此同时，由于返回类型是 Reader 接口，调用方无法通过类型断言
// 取回 PluginCoordinator 并调用写操作，在编译期阻断误用。
func (v *managerInfoView) Coordinator() engine.Reader {
	return v.m.Coordinator()
}

func (v *managerInfoView) GetStatus(name string) *Status {
	s, err := v.m.GetStatus(name)
	if err != nil {
		return nil
	}
	return s
}

// nullPluginInfo nil-safe 空实现（测试场景，Manager 为 nil 时使用）
type nullPluginInfo struct{}

func (n *nullPluginInfo) IsLoaded(_ string) bool                 { return false }
func (n *nullPluginInfo) IsDisabled(_ string) bool               { return false }
func (n *nullPluginInfo) GetStatus(_ string) *Status             { return nil }
func (n *nullPluginInfo) List() []string                         { return nil }
func (n *nullPluginInfo) Count() int                             { return 0 }
func (n *nullPluginInfo) GetMetadata(_ string) (*Metadata, bool) { return nil, false }
func (n *nullPluginInfo) ListWithMetadata() map[string]*Metadata { return nil }
func (n *nullPluginInfo) GetLoadOrder() []string                 { return nil }
func (n *nullPluginInfo) Get(_ string) (*Instance, bool)         { return nil, false }
func (n *nullPluginInfo) Coordinator() engine.Reader             { return nil }
