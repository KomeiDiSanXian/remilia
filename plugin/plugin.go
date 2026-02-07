package plugin

import (
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// Metadata 插件元数据
type Metadata struct {
	// 基本信息
	Name        string // 插件名称
	Version     string // 版本号
	Author      string // 作者
	Description string // 描述
	HelpText    string // 帮助文本

	// 分类和标签
	Category string   // 分类（如 "管理"、"娱乐"、"工具"）
	Tags     []string // 标签

	// 依赖信息
	Dependencies []string // 依赖的插件列表

	// 可见性
	Hidden bool // 是否在帮助中隐藏

	// 联系方式
	Homepage   string // 主页
	Repository string // 仓库地址
}

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

// MetadataProvider 插件元数据提供者接口（可选实现）
// 插件可以实现此接口来提供详细的元数据信息
type MetadataProvider interface {
	// Metadata 返回插件的元数据
	Metadata() *Metadata
}

// ConfigurablePlugin 可配置插件接口（可选实现）
// 实现此接口的插件支持配置管理
type ConfigurablePlugin interface {
	// GetConfig 获取插件配置
	GetConfig() Config

	// SetConfig 设置插件配置（由 Manager 调用）
	SetConfig(config Config)
}

// StatefulPlugin 有状态插件接口（可选实现）
// 实现此接口的插件支持状态查询
type StatefulPlugin interface {
	// GetState 获取插件状态
	GetState() State

	// SetState 设置插件状态（由 Manager 调用）
	SetState(state State)

	// GetLoadTime 获取加载时间
	GetLoadTime() time.Time

	// SetLoadTime 设置加载时间（由 Manager 调用）
	SetLoadTime(t time.Time)

	// GetLastError 获取最后的错误
	GetLastError() error

	// SetLastError 设置最后的错误（由 Manager 调用）
	SetLastError(err error)

	// GetUptime 获取运行时长
	GetUptime() time.Duration
}

// MatcherProvider 提供 Matcher 的插件接口（可选实现）
// 实现此接口的插件可以查询其注册的 Matcher
type MatcherProvider interface {
	// GetMatchers 获取插件注册的所有 Matcher
	GetMatchers() []*engine.Matcher
}

// EventAwarePlugin 事件感知插件接口（可选实现）
// 实现此接口的插件支持事件总线
type EventAwarePlugin interface {
	// PublishEvent 发布事件
	PublishEvent(topic string, data any) error

	// SubscribeEvent 订阅事件
	SubscribeEvent(topic string, handler EventHandler) (Subscription, error)

	// UnsubscribeEvent 取消订阅
	UnsubscribeEvent(sub Subscription) error

	// GetEventBus 获取事件总线
	GetEventBus() EventBus
}

// BasePlugin 基础插件结构
type BasePlugin struct {
	name      string
	metadata  *Metadata
	matchers  []*engine.Matcher
	config    Config
	eventBus  EventBus
	state     State
	loadTime  time.Time
	lastError error
	mu        sync.RWMutex
}

// NewBasePlugin 创建基础插件
func NewBasePlugin(name string) *BasePlugin {
	metadata := &Metadata{
		Name: name,
	}
	return NewBasePluginWithMetadata(metadata)
}

// NewBasePluginWithMetadata 创建带元数据的基础插件
func NewBasePluginWithMetadata(metadata *Metadata) *BasePlugin {
	return &BasePlugin{
		name:     metadata.Name,
		metadata: metadata,
		matchers: make([]*engine.Matcher, 0),
		eventBus: NewEventBus(),
		state:    Unloaded,
	}
}

// Name 返回插件名称
func (p *BasePlugin) Name() string {
	return p.name
}

// Metadata 返回插件的元数据（实现 MetadataProvider 接口）
func (p *BasePlugin) Metadata() *Metadata {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.metadata == nil {
		return &Metadata{
			Name: p.name,
		}
	}
	return p.metadata
}

// SetMetadata 设置插件元数据
func (p *BasePlugin) SetMetadata(metadata *Metadata) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metadata = metadata
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
// 1. 保存插件旧 matchers 快照、Coordinator 状态快照
// 2. 尝试 Unload（清空 matchers 并删除）
// 3. 尝试 Load（创建新的 matchers）
// 4. 如果 Load 失败，通过 Coordinator 的 COW 机制回滚
//
// 优势：
//   - 利用 Engine 的 COW 特性，简化回滚逻辑
//   - 回滚更安全，不会出现状态不一致
func (p *BasePlugin) Reload(coordinator *engine.Engine) error {
	if coordinator == nil {
		return errutil.NewPluginError(p.name, "coordinator is nil")
	}

	// 1. 保存插件旧 matchers 快照
	p.mu.Lock()
	oldMatchers := make([]*engine.Matcher, len(p.matchers))
	copy(oldMatchers, p.matchers)
	p.mu.Unlock()

	// 2. 保存 Coordinator 状态快照
	snapshot := coordinator.Snapshot()

	// 3. 尝试卸载（这会清空 p.matchers 并删除 matchers）
	if err := p.Unload(coordinator); err != nil {
		// Unload 失败，状态未改变
		return errutil.WrapErrorf(err, "unload failed during reload")
	}

	// 4. 尝试加载新状态
	if err := p.Load(coordinator); err != nil {
		// Load 失败，需要回滚
		logger.WithError(err).Warn("[Plugin] Load failed during reload, rolling back")

		// 恢复插件旧 matchers 列表
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

		return errutil.WrapErrorf(err, "load failed during reload, rolled back to previous state")
	}

	// 5. 成功，旧的 matchers 已经被 Unload 删除，不需要额外清理
	logger.WithField("plugin", p.name).Info("[Plugin] Reload successful")
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

// OnCommand 注册命令并自动添加到插件的 Matcher 列表
// 这是一个便捷方法，避免开发者忘记调用 AddMatcher
func (p *BasePlugin) OnCommand(eng *engine.Engine, eventType dto.EventType, cmdPattern string, extraRules ...context.Rule) *engine.Matcher {
	if eng == nil {
		logger.Warn("[Plugin] Engine is nil, cannot register command")
		return nil
	}

	matcher := eng.OnCommand(eventType, cmdPattern, extraRules...)
	p.AddMatcher(matcher)
	return matcher
}

// On 注册自定义规则并自动添加到插件的 Matcher 列表
func (p *BasePlugin) On(eng *engine.Engine, eventType dto.EventType, rules ...context.Rule) *engine.Matcher {
	if eng == nil {
		logger.Warn("[Plugin] Engine is nil, cannot register matcher")
		return nil
	}

	matcher := eng.On(eventType, rules...)
	p.AddMatcher(matcher)
	return matcher
}

// OnAny 注册处理所有事件的规则并自动添加到插件的 Matcher 列表
func (p *BasePlugin) OnAny(eng *engine.Engine, rules ...context.Rule) *engine.Matcher {
	if eng == nil {
		logger.Warn("[Plugin] Engine is nil, cannot register matcher")
		return nil
	}

	matcher := eng.OnAny(rules...)
	p.AddMatcher(matcher)
	return matcher
}

// GetConfig 获取插件配置
func (p *BasePlugin) GetConfig() Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// SetConfig 设置插件配置（由 Manager 调用）
func (p *BasePlugin) SetConfig(config Config) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.config = config
}

// PublishEvent 发布事件
func (p *BasePlugin) PublishEvent(topic string, data any) error {
	return p.eventBus.Publish(topic, data)
}

// SubscribeEvent 订阅事件
func (p *BasePlugin) SubscribeEvent(topic string, handler EventHandler) (Subscription, error) {
	return p.eventBus.Subscribe(topic, handler)
}

// UnsubscribeEvent 取消订阅
func (p *BasePlugin) UnsubscribeEvent(sub Subscription) error {
	return p.eventBus.Unsubscribe(sub)
}

// GetEventBus 获取事件总线（用于高级操作）
func (p *BasePlugin) GetEventBus() EventBus {
	return p.eventBus
}

// GetState 获取插件状态（实现 StatefulPlugin 接口）
func (p *BasePlugin) GetState() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// SetState 设置插件状态（实现 StatefulPlugin 接口）
func (p *BasePlugin) SetState(state State) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = state
}

// GetLoadTime 获取加载时间（实现 StatefulPlugin 接口）
func (p *BasePlugin) GetLoadTime() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.loadTime
}

// SetLoadTime 设置加载时间（实现 StatefulPlugin 接口）
func (p *BasePlugin) SetLoadTime(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loadTime = t
}

// GetLastError 获取最后的错误（实现 StatefulPlugin 接口）
func (p *BasePlugin) GetLastError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastError
}

// SetLastError 设置最后的错误（实现 StatefulPlugin 接口）
func (p *BasePlugin) SetLastError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastError = err
}

// GetUptime 获取运行时长（实现 StatefulPlugin 接口）
func (p *BasePlugin) GetUptime() time.Duration {
	p.mu.RLock()
	loadTime := p.loadTime
	state := p.state
	p.mu.RUnlock()

	if state != Loaded || loadTime.IsZero() {
		return 0
	}

	return time.Since(loadTime)
}
