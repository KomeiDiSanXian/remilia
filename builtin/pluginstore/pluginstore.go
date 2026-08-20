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
//	storeSvc := ctx.Service[*pluginstore.Plugin]("pluginstore")
//	store.RegisterFunc("myplugin",
//	    func() (any, error) { return myState, nil },       // SaveState
//	    func(v any) error { return loadFrom(v) },          // RestoreState
//	)
package pluginstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
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

type registration struct {
	name    string
	save    SaveFunc
	restore RestoreFunc
}

// pluginstoreFile is the on-disk format: map of plugin name → raw JSON state.
type pluginstoreFile struct {
	States map[string]json.RawMessage `json:"states"`
}

// Plugin 插件配置持久化插件
type Plugin struct {
	mu       sync.RWMutex
	regs     map[string]*registration
	fileMu   sync.Mutex // guards read-modify-write on the data file
	dataFile string     // 持久化文件路径（空字符串=无持久化）
	store    kv.Store   // LevelDB 存储（优先级高于 dataFile）
	dryRun   bool       // DryRun 模式下跳过所有 I/O
}

// Option 配置选项
type Option func(*Plugin)

// WithDataFile 设置 JSON 持久化文件路径。空字符串表示禁用持久化。
// 与 WithStore/WithLevelDB 互斥，优先级低于 WithStore。
func WithDataFile(path string) Option {
	return func(p *Plugin) { p.dataFile = path }
}

// WithStore 设置 KV 存储后端（如 LevelDB）。优先级高于 WithDataFile。
func WithStore(s kv.Store) Option {
	return func(p *Plugin) { p.store = s }
}

// NewPlugin 创建 Plugin 实例
func NewPlugin(opts ...Option) *Plugin {
	p := &Plugin{regs: make(map[string]*registration)}
	for _, o := range opts {
		o(p)
	}
	return p
}

// New 创建插件配置持久化插件描述符
func New(opts ...Option) *plugin.Descriptor {
	return NewPlugin(opts...).Descriptor()
}

// Descriptor 从已有 Plugin 创建描述符
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "pluginstore",
		Version: "1.0.0",
		Deps:    []string{},
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
			p.dryRun = ctx.DryRun
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
// 调用后会立即尝试从文件恢复该插件的上一次状态（如果有）。
func (p *Plugin) RegisterFunc(name string, save SaveFunc, restore RestoreFunc) {
	if name == "" || save == nil || restore == nil {
		return
	}
	p.mu.Lock()
	p.regs[name] = &registration{name: name, save: save, restore: restore}
	p.mu.Unlock()
	p.tryRestore(name, restore)
}

// Register 注册实现了 Stateful 接口的对象
func (p *Plugin) Register(name string, s Stateful) {
	p.RegisterFunc(name, s.SaveState, s.RestoreState)
}

// Unregister 注销插件的状态管理
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

func (p *Plugin) doSave(r *registration) error {
	if p.dryRun {
		return nil
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

	if p.store != nil {
		if err := p.store.Set([]byte(r.name), data); err != nil {
			return fmt.Errorf("pluginstore: write for %s failed: %w", r.name, err)
		}
		logger.Debugf("[PluginStore] Saved state for plugin %s (%d bytes)", r.name, len(data))
		return nil
	}

	if p.dataFile == "" {
		return fmt.Errorf("pluginstore: no storage configured")
	}

	p.fileMu.Lock()
	defer p.fileMu.Unlock()
	current, err := jsonfile.Read[pluginstoreFile](p.dataFile)
	if err != nil || current.States == nil {
		current = pluginstoreFile{States: make(map[string]json.RawMessage)}
	}
	current.States[r.name] = data
	if err := jsonfile.Write(p.dataFile, current); err != nil {
		return fmt.Errorf("pluginstore: write for %s failed: %w", r.name, err)
	}
	logger.Debugf("[PluginStore] Saved state for plugin %s (%d bytes)", r.name, len(data))
	return nil
}

func (p *Plugin) tryRestore(name string, restore RestoreFunc) {
	if p.dryRun {
		return
	}
	if p.store != nil {
		raw, err := p.store.Get([]byte(name))
		if errors.Is(err, kv.ErrNotFound) {
			return
		}
		if err != nil {
			logger.WithError(err).Warnf("[PluginStore] Failed to read state for plugin %s", name)
			return
		}
		p.restoreFromRaw(name, raw, restore)
		return
	}
	if p.dataFile == "" {
		return
	}
	p.fileMu.Lock()
	current, err := jsonfile.Read[pluginstoreFile](p.dataFile)
	p.fileMu.Unlock()
	if err != nil {
		return
	}
	raw, ok := current.States[name]
	if !ok {
		return
	}
	p.restoreFromRaw(name, raw, restore)
}

func (p *Plugin) restoreFromRaw(name string, raw []byte, restore RestoreFunc) {
	var state any
	if err := json.Unmarshal(raw, &state); err != nil {
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

// HasStorage 报告是否已配置持久化后端。
func (p *Plugin) HasStorage() bool {
	return p.store != nil || p.dataFile != ""
}

// SetDataFileForTest 直接设置数据文件路径（仅用于测试）。
func (p *Plugin) SetDataFileForTest(path string) {
	p.dataFile = path
}
