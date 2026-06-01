package plugin

import (
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// manager_config.go — 插件配置控制器

// configController 管理全局配置提供者、严格依赖模式。
type configController struct {
	pm            *Manager
	configProvider ConfigProvider
	strictDeps    bool
}

func newConfigController(pm *Manager) *configController {
	return &configController{pm: pm}
}

// SetProvider 设置全局配置提供者并订阅变更事件。
//
// 并发安全说明：
//   - 旧 provider 的 Stop() 在锁外执行（防止 Stop 中 I/O 阻塞其他操作）
//   - Config 构造（NewPluginConfigFromProvider → Sub()）在锁外执行（可能 I/O）
//   - 锁内仅执行指针赋值：configProvider 替换、inst.SetConfig（字段赋值）
//   - 与注册的三段锁可安全并发
func (cc *configController) SetProvider(cp ConfigProvider) {
	// Phase 1: 断开旧 provider 的监听（锁外执行 Stop）
	var oldStopFn func()
	cc.pm.mu.Lock()
	if oldProvider := cc.configProvider; oldProvider != nil {
		if s, ok := oldProvider.(interface{ Stop() }); ok {
			oldStopFn = s.Stop
		}
	}
	cc.pm.mu.Unlock()
	if oldStopFn != nil {
		oldStopFn()
	}

	// Phase 2: 替换 provider + 收集插件名（锁内，短操作）
	cc.pm.mu.Lock()
	cc.configProvider = cp
	names := make([]string, 0, len(cc.pm.plugins))
	for name := range cc.pm.plugins {
		names = append(names, name)
	}
	cc.pm.mu.Unlock()

	// Phase 3: 构造 Config（锁外，可能 I/O）
	type nameConfig struct {
		name   string
		config Config
	}
	newCfgs := make([]nameConfig, 0, len(names))
	if cp != nil {
		for _, name := range names {
			newCfgs = append(newCfgs, nameConfig{name, NewPluginConfigFromProvider(name, cp)})
		}
	}

	// Phase 4: 应用 Config + 注册回调（锁内）
	cc.pm.mu.Lock()
	if cp != nil {
		for _, nc := range newCfgs {
			if inst, ok := cc.pm.plugins[nc.name]; ok {
				inst.SetConfig(nc.config)
			}
		}
		cp.OnConfigChange(cc.propagateConfigChange)
	}
	cc.pm.mu.Unlock()
}

// propagateConfigChange 按依赖顺序向所有插件广播配置变更。
//
// 使用 loadOrder（拓扑排序结果）确保依赖先于依赖方收到变更通知。
// 使用 TryRLock 避免 SetProvider 持有写锁时同步触发导致死锁。
// 若锁被写操作持有，直接返回（Phase 3 的全量替换已保证数据最新）。
func (cc *configController) propagateConfigChange() {
	if !cc.pm.mu.TryRLock() {
		logger.Warn("[Manager] Config change notification skipped: write lock held (SetProvider in progress, Phase 3 full replacement already up to date)")
		return
	}
	defer cc.pm.mu.RUnlock()

	for _, name := range cc.pm.loadOrder {
		inst, exists := cc.pm.plugins[name]
		if !exists {
			continue
		}
		cfg := inst.GetConfig()
		if cfg != nil {
			if err := cfg.Reload(); err != nil {
				logger.WithError(err).Warnf("[Manager] Failed to reload config for plugin %s", name)
			}
		}
	}
}

// SetStrictDeps 设置严格依赖模式。
func (cc *configController) SetStrictDeps(enabled bool) {
	cc.pm.mu.Lock()
	cc.strictDeps = enabled
	cc.pm.mu.Unlock()
}

// IsStrictDeps 返回当前严格依赖模式状态。
func (cc *configController) IsStrictDeps() bool {
	cc.pm.mu.RLock()
	defer cc.pm.mu.RUnlock()
	return cc.strictDeps
}
