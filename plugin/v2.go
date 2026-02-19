package plugin

import (
	"fmt"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// PluginDescriptor 插件描述符（v2 简化 API）
//
// 使用函数式方法定义插件，无需继承，无需实现复杂接口。
// 推荐使用此方式创建新插件。
//
// 示例：
//
//	func NewMyPlugin() *PluginDescriptor {
//	    return &PluginDescriptor{
//	        Name:    "myplugin",
//	        Version: "1.0.0",
//	        Deps:    []string{"permission"},
//	        Setup: func(ctx *SetupContext) error {
//	            perm := ctx.MustGet("permission").(*permission.Plugin)
//	            ctx.Engine.OnCommand(dto.C2CMessageCreate, "/hello").
//	                Handle(func(c *eventctx.Context) error {
//	                    return c.Reply("Hello!")
//	                })
//	            return nil
//	        },
//	    }
//	}
type PluginDescriptor struct {
	// 基本信息
	Name        string // 插件名称（必需）
	Version     string // 版本号
	Author      string // 作者
	Description string // 描述
	HelpText    string // 帮助文本

	// 分类和标签
	Category string   // 分类
	Tags     []string // 标签

	// 依赖
	Deps []string // 依赖的插件列表

	// 生命周期钩子
	Setup    SetupFunc    // 初始化函数（必需）
	Teardown TeardownFunc // 清理函数（可选）
	Reload   ReloadFunc   // 热重载函数（可选）

	// 配置
	ConfigSchema any // 配置结构（可选）

	// 可见性
	Hidden bool // 是否在帮助中隐藏
}

// SetupFunc 插件初始化函数
// 插件应在此函数中：
//   - 注册命令和事件处理器
//   - 获取依赖插件
//   - 初始化内部状态
//   - 读取配置
type SetupFunc func(ctx *SetupContext) error

// TeardownFunc 插件清理函数
// 插件应在此函数中：
//   - 释放资源
//   - 停止后台任务
//   - 保存状态
type TeardownFunc func() error

// ReloadFunc 插件热重载函数
// 实现热重载逻辑（可选）
// 如果不实现，将使用默认的 Teardown + Setup 策略
type ReloadFunc func(ctx *SetupContext) error

// SetupContext 插件初始化上下文
// 提供插件初始化所需的所有资源
type SetupContext struct {
	Engine  *engine.Engine // 事件引擎
	Manager *Manager       // 插件管理器
	Config  Config         // 插件配置

	container  *Container      // 依赖注入容器
	pluginName string          // 当前插件名称（内部使用）
	instance   *PluginInstance // 插件实例引用（内部使用）
}

// Get 获取依赖插件
// 返回插件实例和是否存在的标志
func (ctx *SetupContext) Get(name string) (any, bool) {
	if ctx.container == nil {
		return nil, false
	}
	return ctx.container.Get(name)
}

// MustGet 获取依赖插件（如果不存在则 panic）
// 用于必需的依赖
func (ctx *SetupContext) MustGet(name string) any {
	plugin, ok := ctx.Get(name)
	if !ok {
		panic(fmt.Sprintf("required dependency '%s' not found", name))
	}
	return plugin
}

// RegisterCommand 注册命令并自动追踪 Matcher
// 推荐使用此方法替代直接调用 Engine.OnCommand，以便插件系统能够追踪注册的 Matcher
func (ctx *SetupContext) RegisterCommand(eventType dto.EventType, pattern string, extraRules ...context.Rule) *engine.Matcher {
	matcher := ctx.Engine.OnCommand(eventType, pattern, extraRules...)

	if matcher != nil && ctx.pluginName != "" {
		matcher.SetGroup(ctx.pluginName)
		matcher.SetSource("plugin:" + ctx.pluginName)

		// 追踪 matcher
		if ctx.instance != nil {
			ctx.instance.addMatcher(matcher)
		}
	}

	return matcher
}

// RegisterMatcher 注册自定义 Matcher 并追踪
// 推荐使用此方法替代直接调用 Engine.On，以便插件系统能够追踪注册的 Matcher
func (ctx *SetupContext) RegisterMatcher(eventType dto.EventType, rules ...context.Rule) *engine.Matcher {
	matcher := ctx.Engine.On(eventType, rules...)

	if matcher != nil && ctx.pluginName != "" {
		matcher.SetGroup(ctx.pluginName)
		matcher.SetSource("plugin:" + ctx.pluginName)

		// 追踪 matcher
		if ctx.instance != nil {
			ctx.instance.addMatcher(matcher)
		}
	}

	return matcher
}

// GetPlugin 获取依赖插件（类型安全版本）
// 自动进行类型转换，如果类型不匹配则返回错误
func GetPlugin[T any](ctx *SetupContext, name string) (*T, error) {
	plugin, ok := ctx.Get(name)
	if !ok {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}

	typed, ok := plugin.(*T)
	if !ok {
		return nil, fmt.Errorf("plugin '%s' has wrong type: expected *%T, got %T", name, typed, plugin)
	}

	return typed, nil
}

// MustGetPlugin 获取依赖插件（类型安全版本，失败则 panic）
func MustGetPlugin[T any](ctx *SetupContext, name string) *T {
	plugin, err := GetPlugin[T](ctx, name)
	if err != nil {
		panic(err)
	}
	return plugin
}

// Container 依赖注入容器
type Container struct {
	services map[string]any
	mu       sync.RWMutex
}

// NewContainer 创建依赖注入容器
func NewContainer() *Container {
	return &Container{
		services: make(map[string]any),
	}
}

// Register 注册服务
func (c *Container) Register(name string, service any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.services[name] = service
}

// Get 获取服务
func (c *Container) Get(name string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	service, ok := c.services[name]
	return service, ok
}

// Has 检查服务是否存在
func (c *Container) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.services[name]
	return ok
}

// Remove 移除服务
func (c *Container) Remove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.services, name)
}

// PluginInstance v2 插件实例
type PluginInstance struct {
	desc         *PluginDescriptor
	state        State
	setupContext *SetupContext
	matchers     []*engine.Matcher // 插件注册的匹配器
	loadTime     time.Time         // 加载时间
	lastError    error             // 最后的错误
	mu           sync.RWMutex
}

// Name 返回插件名称
func (pi *PluginInstance) Name() string {
	return pi.desc.Name
}

// Load 加载插件（实现 Plugin 接口，用于兼容）
func (pi *PluginInstance) Load(coordinator *engine.Engine) error {
	pi.mu.Lock()
	pi.state = Loading
	pi.mu.Unlock()

	startTime := time.Now()

	// 调用 Setup 函数
	if err := pi.desc.Setup(pi.setupContext); err != nil {
		pi.mu.Lock()
		pi.state = Error
		pi.lastError = err
		pi.mu.Unlock()
		return err
	}

	pi.mu.Lock()
	pi.state = Loaded
	pi.loadTime = startTime
	pi.lastError = nil
	pi.mu.Unlock()

	return nil
}

// Unload 卸载插件（实现 Plugin 接口，用于兼容）
func (pi *PluginInstance) Unload(coordinator *engine.Engine) error {
	pi.mu.Lock()
	pi.state = Unloaded // Changed from Unloading to Unloaded
	pi.mu.Unlock()

	// 清理注册的匹配器
	if coordinator != nil {
		coordinator.RemoveGroup(pi.desc.Name)
	}

	// 调用 Teardown 函数（如果定义）
	var err error
	if pi.desc.Teardown != nil {
		err = pi.desc.Teardown()
	}

	pi.mu.Lock()
	if err != nil {
		pi.state = Error
	} else {
		pi.state = Unloaded
	}
	pi.mu.Unlock()

	return err
}

// Reload 重载插件（实现 Plugin 接口，用于兼容）
func (pi *PluginInstance) Reload(coordinator *engine.Engine) error {
	pi.mu.Lock()
	oldContext := pi.setupContext
	pi.state = Reloading
	pi.mu.Unlock()

	// 重新创建 SetupContext 以获取最新的容器状态
	newContext := &SetupContext{
		Engine:     oldContext.Engine,
		Manager:    oldContext.Manager,
		Config:     oldContext.Config,
		container:  oldContext.container,
		pluginName: oldContext.pluginName,
		instance:   oldContext.instance,
	}

	pi.mu.Lock()
	pi.setupContext = newContext
	pi.mu.Unlock()

	// 如果定义了自定义 Reload 函数，使用它
	if pi.desc.Reload != nil {
		if err := pi.desc.Reload(newContext); err != nil {
			pi.mu.Lock()
			pi.state = Error
			pi.lastError = err
			pi.mu.Unlock()
			return err
		}

		pi.mu.Lock()
		pi.state = Loaded
		pi.loadTime = time.Now()
		pi.lastError = nil
		pi.mu.Unlock()

		return nil
	}

	// 默认策略：Unload + Load
	if err := pi.Unload(coordinator); err != nil {
		return err
	}
	return pi.Load(coordinator)
}

// Dependencies 返回依赖列表（实现 Plugin 接口，用于兼容）
func (pi *PluginInstance) Dependencies() []string {
	return pi.desc.Deps
}

// Metadata 返回元数据（实现 MetadataProvider 接口）
func (pi *PluginInstance) Metadata() *Metadata {
	return &Metadata{
		Name:         pi.desc.Name,
		Version:      pi.desc.Version,
		Author:       pi.desc.Author,
		Description:  pi.desc.Description,
		HelpText:     pi.desc.HelpText,
		Category:     pi.desc.Category,
		Tags:         pi.desc.Tags,
		Dependencies: pi.desc.Deps,
		Hidden:       pi.desc.Hidden,
	}
}

// GetState 获取插件状态（实现 StatefulPlugin 接口）
func (pi *PluginInstance) GetState() State {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.state
}

// SetState 设置插件状态（实现 StatefulPlugin 接口）
func (pi *PluginInstance) SetState(state State) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.state = state
}

// GetLoadTime 获取加载时间（实现 StatefulPlugin 接口）
func (pi *PluginInstance) GetLoadTime() time.Time {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.loadTime
}

// SetLoadTime 设置加载时间（实现 StatefulPlugin 接口）
func (pi *PluginInstance) SetLoadTime(t time.Time) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.loadTime = t
}

// GetLastError 获取最后的错误（实现 StatefulPlugin 接口）
func (pi *PluginInstance) GetLastError() error {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	return pi.lastError
}

// SetLastError 设置最后的错误（实现 StatefulPlugin 接口）
func (pi *PluginInstance) SetLastError(err error) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.lastError = err
}

// GetUptime 获取运行时长（实现 StatefulPlugin 接口）
func (pi *PluginInstance) GetUptime() time.Duration {
	pi.mu.RLock()
	loadTime := pi.loadTime
	state := pi.state
	pi.mu.RUnlock()

	if state != Loaded || loadTime.IsZero() {
		return 0
	}

	return time.Since(loadTime)
}

// GetMatchers 获取插件注册的所有匹配器（实现 MatcherProvider 接口）
func (pi *PluginInstance) GetMatchers() []*engine.Matcher {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	result := make([]*engine.Matcher, len(pi.matchers))
	copy(result, pi.matchers)
	return result
}

// addMatcher 添加 Matcher 到追踪列表（内部方法）
func (pi *PluginInstance) addMatcher(matcher *engine.Matcher) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.matchers = append(pi.matchers, matcher)
}

// GetConfig 获取插件配置（实现 ConfigurablePlugin 接口）
func (pi *PluginInstance) GetConfig() Config {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	if pi.setupContext != nil {
		return pi.setupContext.Config
	}
	return nil
}

// SetConfig 设置插件配置（实现 ConfigurablePlugin 接口）
func (pi *PluginInstance) SetConfig(config Config) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if pi.setupContext != nil {
		pi.setupContext.Config = config
	}
}

// RegisterV2 注册 v2 风格的插件（使用 PluginDescriptor）
func (pm *Manager) RegisterV2(desc *PluginDescriptor) error {
	if desc == nil {
		return fmt.Errorf("plugin descriptor is nil")
	}

	if desc.Name == "" {
		return fmt.Errorf("plugin name is required")
	}

	if desc.Setup == nil {
		return fmt.Errorf("plugin setup function is required")
	}

	name := desc.Name

	pm.mu.Lock()

	// 检查重复
	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		logger.Warnf("[pluginManager] Plugin %s already registered", name)
		return errutil.ErrPluginAlreadyExists
	}

	// 检查依赖
	for _, dep := range desc.Deps {
		if _, exists := pm.plugins[dep]; !exists {
			pm.mu.Unlock()
			return fmt.Errorf("missing dependency: %s", dep)
		}
	}

	// 创建依赖注入容器（如果还没有）
	if pm.container == nil {
		pm.container = NewContainer()
	}

	// 将已注册的插件添加到容器
	for pluginName, plugin := range pm.plugins {
		if !pm.container.Has(pluginName) {
			pm.container.Register(pluginName, plugin)
		}
	}

	// 添加特殊服务
	pm.container.Register("manager", pm)
	pm.container.Register("engine", pm.coordinator)
	pm.container.Register("coordinator", pm.coordinator)

	// 创建插件配置
	var config Config
	if pm.viper != nil {
		config = NewPluginConfig(name, pm.viper)
	}

	// 创建插件实例
	instance := &PluginInstance{
		desc:     desc,
		state:    Unloaded,
		matchers: make([]*engine.Matcher, 0),
	}

	// 创建 SetupContext（设置插件名称和实例引用以支持 Matcher 追踪）
	setupCtx := &SetupContext{
		Engine:     pm.coordinator,
		Manager:    pm,
		Config:     config,
		container:  pm.container,
		pluginName: name,
		instance:   instance,
	}

	instance.setupContext = setupCtx

	// 先添加到 plugins map（标记为占位，防止并发注册相同插件）
	pm.plugins[name] = instance

	pm.mu.Unlock()

	// 加载插件（在锁外执行，避免长时间持锁）
	loadErr := instance.Load(pm.coordinator)

	pm.mu.Lock()

	if loadErr != nil {
		// 加载失败，回滚
		delete(pm.plugins, name)
		pm.container.Remove(name)
		pm.mu.Unlock()

		logger.WithError(loadErr).Errorf("[pluginManager] Failed to load plugin %s", name)
		pm.notifyError(name, "load", loadErr)
		return loadErr
	}

	// 加载成功，完成注册
	pm.loadOrder = append(pm.loadOrder, name)
	pm.container.Register(name, instance)

	pm.mu.Unlock()

	logger.Infof("[pluginManager] Plugin %s registered (v2)", name)
	pm.notifyLoaded(name)
	return nil
}

// RegisterMultipleV2 批量注册多个 v2 插件，自动处理依赖顺序
//
// 此方法会：
// 1. 检测循环依赖（使用拓扑排序算法）
// 2. 按正确的依赖顺序注册插件
// 3. 如果任何插件注册失败，已注册的插件不会自动回滚
//
// 使用示例：
//
//	plugins := []*PluginDescriptor{
//	    NewPluginA(), // 依赖 B
//	    NewPluginB(), // 依赖 C
//	    NewPluginC(), // 无依赖
//	}
//	if err := manager.RegisterMultipleV2(plugins); err != nil {
//	    log.Fatal(err)
//	}
func (pm *Manager) RegisterMultipleV2(descriptors []*PluginDescriptor) error {
	if len(descriptors) == 0 {
		return nil
	}

	// 验证所有描述符
	for i, desc := range descriptors {
		if desc == nil {
			return fmt.Errorf("descriptor at index %d is nil", i)
		}
		if desc.Name == "" {
			return fmt.Errorf("descriptor at index %d has empty name", i)
		}
		if desc.Setup == nil {
			return fmt.Errorf("descriptor %s has no setup function", desc.Name)
		}
	}

	// 拓扑排序，检测循环依赖
	sorted, err := pm.topologicalSortV2(descriptors)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}

	// 按依赖顺序注册
	for _, desc := range sorted {
		if err := pm.RegisterV2(desc); err != nil {
			return fmt.Errorf("failed to register plugin %s: %w", desc.Name, err)
		}
	}

	logger.Infof("[pluginManager] Successfully registered %d plugins in dependency order", len(sorted))
	return nil
}

// topologicalSortV2 使用 Kahn 算法进行拓扑排序
// 返回按依赖顺序排列的插件列表，如果存在循环依赖则返回错误
func (pm *Manager) topologicalSortV2(descriptors []*PluginDescriptor) ([]*PluginDescriptor, error) {
	// 构建映射：名称 -> 描述符
	descMap := make(map[string]*PluginDescriptor)
	for _, desc := range descriptors {
		if _, exists := descMap[desc.Name]; exists {
			return nil, fmt.Errorf("duplicate plugin name: %s", desc.Name)
		}
		descMap[desc.Name] = desc
	}

	// 构建依赖图和入度表
	// inDegree[name] = 依赖该插件的数量
	// graph[name] = 依赖于 name 的插件列表
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	// 初始化入度
	for name := range descMap {
		inDegree[name] = 0
		graph[name] = make([]string, 0)
	}

	// 计算入度和构建图
	for _, desc := range descriptors {
		for _, dep := range desc.Deps {
			// 检查依赖是否存在（可能已在 manager 中注册，或在当前批次中）
			pm.mu.RLock()
			_, existsInManager := pm.plugins[dep]
			pm.mu.RUnlock()

			_, existsInBatch := descMap[dep]

			if !existsInManager && !existsInBatch {
				return nil, fmt.Errorf("plugin %s has missing dependency: %s", desc.Name, dep)
			}

			// 只处理批次内的依赖关系
			if existsInBatch {
				inDegree[desc.Name]++
				graph[dep] = append(graph[dep], desc.Name)
			}
		}
	}

	// Kahn 算法：拓扑排序
	queue := make([]string, 0)

	// 找出所有入度为 0 的节点（无依赖或依赖已满足）
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	result := make([]*PluginDescriptor, 0, len(descriptors))
	processed := 0

	for len(queue) > 0 {
		// 取出一个入度为 0 的节点
		current := queue[0]
		queue = queue[1:]

		// 添加到结果
		result = append(result, descMap[current])
		processed++

		// 减少所有依赖于 current 的节点的入度
		for _, dependent := range graph[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// 检查是否所有节点都被处理（如果没有，说明存在循环依赖）
	if processed != len(descriptors) {
		// 找出形成循环的插件
		unprocessed := make([]string, 0)
		for name, degree := range inDegree {
			if degree > 0 {
				unprocessed = append(unprocessed, name)
			}
		}
		return nil, fmt.Errorf("circular dependency detected among plugins: %v", unprocessed)
	}

	return result, nil
}

// ValidateDependencies 验证一组插件的依赖关系（不注册）
// 返回错误如果存在循环依赖或缺失依赖
//
// 使用示例：
//
//	if err := manager.ValidateDependencies(plugins); err != nil {
//	    log.Printf("Dependency validation failed: %v", err)
//	}
func (pm *Manager) ValidateDependencies(descriptors []*PluginDescriptor) error {
	_, err := pm.topologicalSortV2(descriptors)
	return err
}
