package plugin

import (
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
)

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

	// Get 获取指定插件的只读快照（不含任何状态变更方法），不存在时返回零值, false
	Get(name string) (InstanceInfo, bool)

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

// Get 返回只读快照：调用方拿不到 *Instance，也就无法调用 SetState/SetConfig
// 等导出写方法。
func (v *managerInfoView) Get(name string) (InstanceInfo, bool) {
	inst, ok := v.m.Get(name)
	if !ok || inst == nil {
		return InstanceInfo{}, false
	}
	return inst.infoSnapshot(), true
}

// Coordinator 返回包装后的只读视图，防止通过类型断言绕过只读限制。
//
// PluginCoordinator 已嵌入 Reader，可直接作为只读视图返回。
// 与此同时，由于返回类型是 Reader 接口，调用方无法通过类型断言
// 取回 PluginCoordinator 并调用写操作，在编译期阻断误用。
func (v *managerInfoView) Coordinator() engine.Reader {
	return readOnlyCoordinator{v.m.Coordinator()}
}

// readOnlyCoordinator 只暴露 engine.Reader 的只读视图。
//
// 容器里的 "engine"/"coordinator" 键用它代替原始 coordinator：即便调用方
// 通过类型断言拿到这个值，也只能读到查询接口，拿不到 matcher/group 写能力。
type readOnlyCoordinator struct{ engine.Reader }

// InstanceInfo 插件实例的只读快照：只含查询方法，不暴露任何状态变更入口。
type InstanceInfo struct {
	name      string
	state     State
	loadTime  time.Time
	lastError error
}

// Name 返回插件名。
func (i InstanceInfo) Name() string { return i.name }

// GetState 返回快照时刻的生命周期状态。
func (i InstanceInfo) GetState() State { return i.state }

// GetUptime 返回自加载以来的时长（未加载过返回 0）。
func (i InstanceInfo) GetUptime() time.Duration {
	if i.loadTime.IsZero() {
		return 0
	}
	return time.Since(i.loadTime)
}

// GetLastError 返回最近一次错误（无则 nil）。
func (i InstanceInfo) GetLastError() error { return i.lastError }

// Info 返回实例的只读快照。
func (pi *Instance) Info() InstanceInfo { return pi.infoSnapshot() }

// infoSnapshot 在实例锁下复制只读字段。
func (pi *Instance) infoSnapshot() InstanceInfo {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	info := InstanceInfo{
		state:     pi.state,
		loadTime:  pi.loadTime,
		lastError: pi.lastError,
	}
	if pi.desc != nil {
		info.name = pi.desc.Name
	}
	return info
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
func (n *nullPluginInfo) Get(_ string) (InstanceInfo, bool)      { return InstanceInfo{}, false }
func (n *nullPluginInfo) Coordinator() engine.Reader             { return nil }
