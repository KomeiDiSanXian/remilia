package plugin

import (
	"errors"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/sirupsen/logrus"
)

// 插件相关错误
var (
	ErrPluginAlreadyExists = errors.New("plugin already exists")
	ErrPluginNotFound      = errors.New("plugin not found")
	ErrCircularDependency  = errors.New("circular dependency detected")
	ErrDependencyNotFound  = errors.New("dependency not found")
)

// Plugin 插件接口
type Plugin interface {
	// Name 返回插件名称
	Name() string

	// Load 加载插件到引擎，返回错误信息
	Load(coordinator *engine.Engine) error

	// Unload 卸载插件，返回错误信息
	Unload(coordinator *engine.Engine) error

	// Reload 原子性重载插件（策略 B）：
	//  - 成功时，用新的内部状态替换旧状态；
	//  - 失败时，不改变原有状态（调用方可根据错误自行处理）。
	// coordinator 参数用于重新注册 handler 等操作
	Reload(coordinator *engine.Engine) error

	// Dependencies 返回插件依赖列表（v0.7.1 新增）
	// 返回的插件名称列表表示此插件依赖的其他插件
	// 插件管理器会确保依赖的插件先于当前插件加载
	Dependencies() []string
}

// BasePlugin 基础插件结构
type BasePlugin struct {
	name     string
	matchers []*engine.Matcher
	mu       sync.RWMutex
}

// NewBasePlugin 创建基础插件
func NewBasePlugin(name string) *BasePlugin {
	return &BasePlugin{
		name:     name,
		matchers: make([]*engine.Matcher, 0),
	}
}

// Name 返回插件名称
func (p *BasePlugin) Name() string {
	return p.name
}

// AddMatcher 添加匹配器到插件（线程安全）
func (p *BasePlugin) AddMatcher(matcher *engine.Matcher) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Contract:
	// - matcher.group is the authoritative grouping key used for middleware scoping and plugin unloading.
	// - matcher.Source is diagnostics/labeling only.
	source := "plugin:" + p.name

	if matcher != nil {
		// 设置匹配器的分组和来源
		matcher.SetSource(source)
		matcher.SetGroup(p.name)
	}

	p.matchers = append(p.matchers, matcher)
}

// GetMatchers 获取所有匹配器（线程安全）
func (p *BasePlugin) GetMatchers() []*engine.Matcher {
	p.mu.RLock()
	defer p.mu.RUnlock()
	matchers := make([]*engine.Matcher, len(p.matchers))
	copy(matchers, p.matchers)
	return matchers
}

// Load 加载插件（子类需要重写实现具体逻辑）
func (p *BasePlugin) Load(_ *engine.Engine) error {
	// 默认实现为空，子类重写
	return nil
}

// Unload 卸载插件，清理所有匹配器（在锁外删除匹配器，避免锁反转）
func (p *BasePlugin) Unload(coordinator *engine.Engine) error {
	if coordinator != nil {
		coordinator.RemoveGroup(p.name)
	}

	p.mu.Lock()
	p.matchers = make([]*engine.Matcher, 0)
	p.mu.Unlock()

	return nil
}

// Reload 的默认实现：原子性重载插件（适配 COW Engine）
//
// COW Engine 下的实现策略：
// 1. 保存插件的 matchers 快照和 Coordinator 状态快照
// 2. 尝试 Unload（清空 matchers 并删除）
// 3. 尝试 Load（创建新的 matchers）
// 4. 如果 Load 失败，通过 Coordinator 的 COW 机制回滚
//
// 优势：
//   - 利用 Engine 的 COW 特性，简化回滚逻辑
//   - 回滚更安全，不会出现状态不一致
func (p *BasePlugin) Reload(coordinator *engine.Engine) error {
	if coordinator == nil {
		return fmt.Errorf("coordinator is nil")
	}

	// 1. 保存插件的 matchers 快照
	p.mu.Lock()
	oldMatchers := make([]*engine.Matcher, len(p.matchers))
	copy(oldMatchers, p.matchers)
	p.mu.Unlock()

	// 2. 保存 Coordinator 状态快照
	snapshot := coordinator.Snapshot()

	// 3. 尝试卸载（这会清空 p.matchers 并删除 matchers）
	if err := p.Unload(coordinator); err != nil {
		// Unload 失败，状态未改变
		return fmt.Errorf("unload failed during reload: %w", err)
	}

	// 4. 尝试加载新状态
	if err := p.Load(coordinator); err != nil {
		// Load 失败，需要回滚
		logrus.WithError(err).Warn("[Plugin] Load failed during reload, rolling back")

		// 恢复插件的 matchers 列表
		p.mu.Lock()
		p.matchers = oldMatchers
		p.mu.Unlock()

		// 回滚 Coordinator 状态
		coordinator.Restore(snapshot)

		// 重建中间件链
		for _, matcher := range oldMatchers {
			if matcher != nil {
				coordinator.RebuildMatcherChain(matcher)
			}
		}

		return fmt.Errorf("load failed during reload, rolled back to previous state: %w", err)
	}

	// 5. 成功，旧的 matchers 已经被 Unload 删除，不需要额外清理
	logrus.WithField("plugin", p.name).Info("[Plugin] Reload successful")
	return nil
}

// Dependencies 返回插件依赖列表（默认无依赖）
// 子类可以重写此方法来声明依赖
func (p *BasePlugin) Dependencies() []string {
	return []string{}
}

// Use 为当前插件注册中间件（作用于该插件的所有匹配器）
func (p *BasePlugin) Use(coordinator *engine.Engine, mw ...context.Middleware) {
	if coordinator == nil {
		return
	}
	coordinator.UseForGroup(p.name, mw...)
}
