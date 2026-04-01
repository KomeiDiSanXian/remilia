// Package pluginstore 提供插件配置持久化插件。
//
// 提供统一的"插件状态快照"机制：
//   - 插件通过实现 Stateful 接口（或调用 Register/RegisterFunc）声明自己有可持久化的状态
//   - pluginstore 在 shutdown 时触发所有已注册插件的 SaveState，将结果序列化到 storage
//   - 启动时（Setup）从 storage 加载快照，逐一调用已注册插件的 RestoreState
//
// 与 Descriptor.SaveState/RestoreState 的关系：
//   - Descriptor.SaveState/RestoreState 是热重载专用的内存传递（不持久化）
//   - pluginstore 实现的是跨进程重启的持久化版本
//
// 使用示例:
//
//	pm.Register(storage.New())
//	pm.Register(pluginstore.New())
//
//	// 在你的插件 Setup 中：
//	store := ctx.MustGet("pluginstore").(*pluginstore.Plugin)
//	store.RegisterFunc("myplugin",
//	    func() (any, error) { return myState, nil },       // SaveState
//	    func(v any) error { return loadFrom(v) },          // RestoreState
//	)
package pluginstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	storage "github.com/KomeiDiSanXian/remilia/builtin/core/storage"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Stateful 插件实现此接口后，可被 pluginstore 自动发现并注册（可选）
// 通常更推荐使用 RegisterFunc 手动注册，更加明确。
type Stateful interface {
	// SaveState 返回可被 JSON 序列化的状态快照
	SaveState() (any, error)
	// RestoreState 从反序列化后的 any 恢复状态（通常是 map[string]any）
	RestoreState(state any) error
}

// SaveFunc 保存状态函数
type SaveFunc func() (any, error)

// RestoreFunc 恢复状态函数
type RestoreFunc func(state any) error

// storageBackend 接口已合并至 storage.Client，见 plugins/core/storage

type registration struct {
	name    string
	save    SaveFunc
	restore RestoreFunc
}

// Plugin 插件配置持久化插件
type Plugin struct {
	mu    sync.RWMutex
	regs  map[string]*registration
	store *storage.Store // 命名空间 Store（nil=无持久化）
}

// NewPlugin 创建 Plugin 实例
func NewPlugin() *Plugin {
	return &Plugin{
		regs: make(map[string]*registration),
	}
}

// New 创建插件配置持久化插件描述符
func New() *plugin.Descriptor {
	return Descriptor(NewPlugin())
}

// Descriptor 从已有 Plugin 创建描述符
func Descriptor(p *Plugin) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:         "pluginstore",
		Version:      "1.0.0",
		Deps:         []string{},
		OptionalDeps: []string{"storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "插件配置持久化插件，跨重启保存/恢复插件运行时状态",
			Category:    "系统",
			Tags:        []string{"持久化", "状态", "系统"},
			HelpText: `插件状态持久化使用说明：
  store := plugin.Require[pluginstore.Plugin](ctx, "pluginstore")
  store.RegisterFunc("myplugin", saveFunc, restoreFunc)`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok {
				p.store = sb.NS("pluginstore")
				ctx.Log.Info("Bound to storage plugin")
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ps := ctx.API.(*Plugin)
			saved, failed := ps.SaveAll()
			ctx.Log.Infof("Saved %d plugin states, %d failed", saved, failed)
			return nil
		},
	}
}

// RegisterFunc 注册插件的保存/恢复函数。
//
// 调用后会立即尝试从 storage 恢复该插件的上一次状态（如果有）。
// 适合在插件 Setup 中调用。
func (p *Plugin) RegisterFunc(name string, save SaveFunc, restore RestoreFunc) {
	if name == "" || save == nil || restore == nil {
		return
	}

	p.mu.Lock()
	p.regs[name] = &registration{name: name, save: save, restore: restore}
	p.mu.Unlock()

	// 立即尝试恢复
	p.tryRestore(name, restore)
}

// Register 注册实现了 Stateful 接口的对象
func (p *Plugin) Register(name string, s Stateful) {
	p.RegisterFunc(name, s.SaveState, s.RestoreState)
}

// Unregister 注销插件的状态管理（插件卸载时调用）
func (p *Plugin) Unregister(name string) {
	p.mu.Lock()
	delete(p.regs, name)
	p.mu.Unlock()
}

// Save 保存指定插件的状态
func (p *Plugin) Save(name string) error {
	p.mu.RLock()
	reg, ok := p.regs[name]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("pluginstore: plugin %q not registered", name)
	}
	return p.doSave(reg)
}

// SaveAll 保存所有注册插件的状态，返回成功和失败数量
func (p *Plugin) SaveAll() (saved, failed int) {
	p.mu.RLock()
	regs := make([]*registration, 0, len(p.regs))
	for _, r := range p.regs {
		regs = append(regs, r)
	}
	p.mu.RUnlock()

	for _, r := range regs {
		if err := p.doSave(r); err != nil {
			logger.WithError(err).Warnf("[PluginStore] Failed to save state for plugin %s", r.name)
			failed++
		} else {
			saved++
		}
	}
	return
}

// doSave 执行保存逻辑
func (p *Plugin) doSave(r *registration) error {
	if p.store == nil {
		return fmt.Errorf("pluginstore: no storage backend")
	}

	state, err := r.save()
	if err != nil {
		return fmt.Errorf("pluginstore: SaveState for %s failed: %w", r.name, err)
	}
	if state == nil {
		return nil
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("pluginstore: marshal for %s failed: %w", r.name, err)
	}
	// Use raw bytes since the state type is unknown at compile time.
	if err := p.store.SetRaw(context.Background(), r.name, data, 0); err != nil {
		return fmt.Errorf("pluginstore: store.SetRaw for %s failed: %w", r.name, err)
	}

	logger.Debugf("[PluginStore] Saved state for plugin %s (%d bytes)", r.name, len(data))
	return nil
}

// tryRestore 尝试从 storage 恢复指定插件的状态
func (p *Plugin) tryRestore(name string, restore RestoreFunc) {
	if p.store == nil {
		return
	}

	data, err := p.store.GetRaw(context.Background(), name)
	if err != nil {
		return
	}

	var state any
	if err := json.Unmarshal(data, &state); err != nil {
		logger.WithError(err).Warnf("[PluginStore] Failed to unmarshal state for plugin %s", name)
		return
	}

	if err := restore(state); err != nil {
		logger.WithError(err).Warnf("[PluginStore] Failed to restore state for plugin %s", name)
		return
	}

	logger.Infof("[PluginStore] Restored state for plugin %s", name)
}

// ListRegistered 返回已注册的插件名列表
func (p *Plugin) ListRegistered() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.regs))
	for name := range p.regs {
		names = append(names, name)
	}
	return names
}

// HasStorage 返回是否已绑定 storage 后端
func (p *Plugin) HasStorage() bool {
	return p.store != nil
}

// SetStoreForTest 直接绑定命名空间 Store（仅用于测试）。
func (p *Plugin) SetStoreForTest(store *storage.Store) {
	p.store = store
}
