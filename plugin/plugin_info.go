package plugin

import "github.com/KomeiDiSanXian/remilia/core/engine"

// PluginInfo 插件系统只读视图
//
// 通过 [SetupContext.Info] 访问，允许插件在 Setup 阶段查询其他插件的状态，
// 但不能执行任何写操作（注册/卸载/重载）。
type PluginInfo interface {
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

	// Coordinator 返回底层 engine.Engine（供 help 等需要访问命令列表的插件使用）
	// 比之前通过类型断言访问更安全，明确表示这是只读查询用途
	Coordinator() *engine.Engine
}

// managerInfoView 基于 *Manager 实现 PluginInfo 只读视图
type managerInfoView struct {
	m *Manager
}

func newPluginInfo(m *Manager) PluginInfo {
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
func (v *managerInfoView) Coordinator() *engine.Engine            { return v.m.Coordinator() }

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
func (n *nullPluginInfo) Coordinator() *engine.Engine            { return nil }
