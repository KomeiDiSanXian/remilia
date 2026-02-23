package plugin

import (
	"fmt"
	"sync"
	"sync/atomic"
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

	// 依赖热重载通知回调（可选）
	// 当本插件依赖的某个插件完成热重载后，此函数会被调用。
	// 可用于刷新缓存、重新获取依赖引用等。
	// reloadedDep: 已完成重载的依赖插件名称
	OnDependencyReloaded func(reloadedDep string)

	// 状态保存/恢复钩子（用于热重载）
	SaveState    SaveStateFunc    // 保存状态（可选）
	RestoreState RestoreStateFunc // 恢复状态（可选）

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

// SaveStateFunc 插件状态保存函数
// 在热重载前保存插件状态（可选）
// 返回的状态数据将传递给 RestoreStateFunc
type SaveStateFunc func() (any, error)

// RestoreStateFunc 插件状态恢复函数
// 在热重载后恢复插件状态（可选）
// 接收 SaveStateFunc 返回的状态数据
type RestoreStateFunc func(state any) error

// SetupContext 插件初始化上下文
// 提供插件初始化所需的所有资源
type SetupContext struct {
	Engine   *engine.Engine // 事件引擎
	Manager  *Manager       // 插件管理器
	Config   Config         // 插件配置
	EventBus EventBus       // 插件间事件总线

	// DryRun 为 true 时表示当前处于依赖推断阶段（由 RegisterMultipleV2Smart 使用）。
	// Setup 函数应在 DryRun 为 true 时跳过有副作用的操作（注册命令、启动 goroutine 等），
	// 仅执行 Get/MustGet 调用以暴露依赖关系。
	DryRun bool

	container        *Container      // 依赖注入容器
	pluginName       string          // 当前插件名称（内部使用）
	instance         *PluginInstance // 插件实例引用（内部使用）
	trackedDeps      map[string]bool // 自动跟踪的依赖（内部使用）
	autoTrackEnabled bool            // 是否启用自动依赖跟踪（内部使用）
}

// Get 获取依赖插件
// 返回插件实例和是否存在的标志
//
// 注意：调用此方法会自动记录依赖关系，用于依赖验证
func (ctx *SetupContext) Get(name string) (any, bool) {
	if ctx.container == nil {
		return nil, false
	}

	// 自动跟踪依赖
	if ctx.autoTrackEnabled && name != "" && name != ctx.pluginName {
		if ctx.trackedDeps == nil {
			ctx.trackedDeps = make(map[string]bool)
		}
		ctx.trackedDeps[name] = true
	}

	return ctx.container.Get(name)
}

// MustGet 获取依赖插件（如果不存在则 panic）
// 用于必需的依赖
//
// 注意：调用此方法会自动记录依赖关系，用于依赖验证
func (ctx *SetupContext) MustGet(name string) any {
	plugin, ok := ctx.Get(name)
	if !ok {
		panic(fmt.Sprintf("required dependency '%s' not found", name))
	}
	return plugin
}

// GetTrackedDependencies 获取自动跟踪到的依赖列表
// 返回在 Setup 函数中通过 Get/MustGet 调用的所有插件名称
func (ctx *SetupContext) GetTrackedDependencies() []string {
	if ctx.trackedDeps == nil {
		return []string{}
	}

	deps := make([]string, 0, len(ctx.trackedDeps))
	for name := range ctx.trackedDeps {
		deps = append(deps, name)
	}
	return deps
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
//
// 支持两阶段使用模式：
//  1. 注册阶段（Register/Remove）：使用 sync.Map 保证并发安全
//  2. 冻结阶段（Freeze 后）：Get/Has 切换为无锁只读 map，读性能提升 2-3x
//
// 改进 3.5：插件全部加载完成后调用 Freeze()，后续 Get 无需任何锁操作。
type Container struct {
	services sync.Map // 注册阶段使用

	// 冻结后的只读快照
	frozen    atomic.Bool
	frozenMap map[string]any // 仅在 frozen==true 时访问，无锁读
}

// NewContainer 创建依赖注入容器
func NewContainer() *Container {
	return &Container{}
}

// Register 注册服务。冻结后会自动刷新只读快照，支持热重载/动态注册场景。
func (c *Container) Register(name string, service any) {
	c.services.Store(name, service)
	// 若已冻结，同步刷新快照保持一致性
	if c.frozen.Load() {
		c.refreshSnapshot()
	}
}

// Freeze 将容器切换为只读快照模式。
// 调用后 Get/Has 使用无锁 map，性能提升 2-3x。
// 冻结后仍可调用 Register/Remove，会自动刷新快照。
func (c *Container) Freeze() {
	c.frozen.Store(true)
	c.refreshSnapshot()
}

// refreshSnapshot 重建只读快照（需在 frozen==true 时调用）。
func (c *Container) refreshSnapshot() {
	snapshot := make(map[string]any)
	c.services.Range(func(k, v any) bool {
		snapshot[k.(string)] = v
		return true
	})
	c.frozenMap = snapshot
}

// Get 获取服务。冻结后使用无锁只读 map。
func (c *Container) Get(name string) (any, bool) {
	if c.frozen.Load() {
		v, ok := c.frozenMap[name]
		return v, ok
	}
	return c.services.Load(name)
}

// Has 检查服务是否存在
func (c *Container) Has(name string) bool {
	_, ok := c.Get(name)
	return ok
}

// Remove 移除服务。冻结后会自动刷新只读快照。
func (c *Container) Remove(name string) {
	c.services.Delete(name)
	if c.frozen.Load() {
		c.refreshSnapshot()
	}
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
	pi.state = Unloading
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
		pi.lastError = err
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

	// 保存状态（如果定义了 SaveState 函数）
	var savedState any
	var saveErr error
	if pi.desc.SaveState != nil {
		savedState, saveErr = pi.desc.SaveState()
		if saveErr != nil {
			logger.WithError(saveErr).Warn("[plugin] Failed to save state before reload")
			// 继续重载，但记录错误
		} else {
			logger.Infof("[plugin] State saved for plugin: %s", pi.desc.Name)
		}
	}

	// 重新创建 SetupContext 以获取最新的容器状态
	newContext := &SetupContext{
		Engine:     oldContext.Engine,
		Manager:    oldContext.Manager,
		Config:     oldContext.Config,
		EventBus:   oldContext.EventBus,
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

		// 恢复状态（如果有保存的状态且定义了 RestoreState 函数）
		if savedState != nil && pi.desc.RestoreState != nil {
			if err := pi.desc.RestoreState(savedState); err != nil {
				logger.WithError(err).Warn("[plugin] Failed to restore state after reload")
			} else {
				logger.Infof("[plugin] State restored for plugin: %s", pi.desc.Name)
			}
		}

		return nil
	}

	// 默认策略：Unload + Load
	if err := pi.Unload(coordinator); err != nil {
		return err
	}
	if err := pi.Load(coordinator); err != nil {
		return err
	}

	// 恢复状态（如果有保存的状态且定义了 RestoreState 函数）
	if savedState != nil && pi.desc.RestoreState != nil {
		if err := pi.desc.RestoreState(savedState); err != nil {
			logger.WithError(err).Warn("[plugin] Failed to restore state after reload")
		} else {
			logger.Infof("[plugin] State restored for plugin: %s", pi.desc.Name)
		}
	}

	return nil
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
		depPlugin, exists := pm.plugins[dep]
		if !exists {
			pm.mu.Unlock()
			return fmt.Errorf("missing dependency: %s", dep)
		}
		// 验证依赖插件已完成加载（状态为 Loaded），防止并发注册时依赖方
		// 获取到处于 Loading 状态的插件实例，导致 Setup 中 MustGet 行为异常
		if stateful, ok := depPlugin.(StatefulPlugin); ok {
			state := stateful.GetState()
			if state != Loaded {
				pm.mu.Unlock()
				return fmt.Errorf("dependency '%s' is not ready (state: %s), please register plugins in dependency order", dep, state)
			}
		}
	}

	// 确保容器已初始化
	pm.ensureContainerInitialized()

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
		Engine:           pm.coordinator,
		Manager:          pm,
		Config:           config,
		EventBus:         pm.eventBus,
		container:        pm.container,
		pluginName:       name,
		instance:         instance,
		autoTrackEnabled: true, // 启用自动依赖跟踪
	}

	instance.setupContext = setupCtx

	// 先添加到 plugins map，并设置为 Loading 状态
	// 这样其他 goroutine 通过 Get() 获取时可以检测到插件正在加载
	instance.state = Loading
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

	// 验证自动跟踪的依赖与声明的依赖是否一致
	trackedDeps := setupCtx.GetTrackedDependencies()
	if len(trackedDeps) > 0 {
		// 检查是否有未声明的依赖
		declaredDeps := make(map[string]bool)
		for _, dep := range desc.Deps {
			declaredDeps[dep] = true
		}

		undeclaredDeps := make([]string, 0)
		for _, tracked := range trackedDeps {
			if !declaredDeps[tracked] {
				undeclaredDeps = append(undeclaredDeps, tracked)
			}
		}

		// 如果有未声明的依赖，记录警告
		if len(undeclaredDeps) > 0 {
			logger.WithFields(logger.Fields{
				"plugin":          name,
				"undeclared_deps": undeclaredDeps,
				"declared_deps":   desc.Deps,
			}).Warn("[pluginManager] Plugin uses dependencies not declared in Deps field")
		}
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
//
// 增强版本：检测批次内和跨批次的循环依赖
func (pm *Manager) topologicalSortV2(descriptors []*PluginDescriptor) ([]*PluginDescriptor, error) {
	// 构建映射：名称 -> 描述符
	descMap := make(map[string]*PluginDescriptor)
	for _, desc := range descriptors {
		if _, exists := descMap[desc.Name]; exists {
			return nil, fmt.Errorf("duplicate plugin name: %s", desc.Name)
		}
		descMap[desc.Name] = desc
	}

	// 检查跨批次循环依赖
	if err := pm.checkCrossBatchCyclicDependency(descriptors, descMap); err != nil {
		return nil, err
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
			depPlugin, existsInManager := pm.plugins[dep]
			pm.mu.RUnlock()

			_, existsInBatch := descMap[dep]

			if !existsInManager && !existsInBatch {
				return nil, fmt.Errorf("plugin %s has missing dependency: %s", desc.Name, dep)
			}

			// 验证已注册的依赖插件状态（批次外的依赖必须已 Loaded）
			if existsInManager && !existsInBatch {
				if stateful, ok := depPlugin.(StatefulPlugin); ok {
					if stateful.GetState() != Loaded {
						return nil, fmt.Errorf("plugin %s dependency '%s' is not ready (state: %s)", desc.Name, dep, stateful.GetState())
					}
				}
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

// checkCrossBatchCyclicDependency 检查跨批次循环依赖
// 即：已注册插件和批次内插件之间是否形成循环
func (pm *Manager) checkCrossBatchCyclicDependency(descriptors []*PluginDescriptor, descMap map[string]*PluginDescriptor) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// 对于批次中的每个插件
	for _, desc := range descriptors {
		// 检查它依赖的每个已注册插件
		for _, depName := range desc.Deps {
			existingPlugin, existsInManager := pm.plugins[depName]
			if !existsInManager {
				continue // 不是已注册插件，跳过
			}

			// 检查已注册插件是否（直接或间接）依赖批次中的插件
			if err := pm.detectCycleThroughExisting(existingPlugin, desc.Name, descMap, make(map[string]bool)); err != nil {
				return fmt.Errorf("cross-batch circular dependency: %w", err)
			}
		}
	}

	return nil
}

// detectCycleThroughExisting 检测已注册插件是否依赖批次中的插件（可能形成循环）
// existingPlugin: 已注册的插件
// targetName: 批次中的插件名称
// batchPlugins: 批次中的插件映射
// visited: 已访问的插件集合（防止无限递归）
func (pm *Manager) detectCycleThroughExisting(existingPlugin Plugin, targetName string, batchPlugins map[string]*PluginDescriptor, visited map[string]bool) error {
	pluginName := existingPlugin.Name()

	// 防止无限递归
	if visited[pluginName] {
		return nil
	}
	visited[pluginName] = true

	// 获取已注册插件的依赖
	deps := existingPlugin.Dependencies()

	for _, dep := range deps {
		// 如果已注册插件依赖批次中的插件，形成循环
		if dep == targetName {
			return fmt.Errorf("plugin %s (registered) depends on %s (in batch), which depends on %s",
				pluginName, dep, pluginName)
		}

		// 如果依赖是批次中的其他插件
		if batchDesc, inBatch := batchPlugins[dep]; inBatch {
			// 检查批次中的这个插件是否依赖 targetName
			if pm.batchPluginDependsOn(batchDesc, targetName, batchPlugins, make(map[string]bool)) {
				return fmt.Errorf("plugin %s (registered) -> %s (batch) -> %s (batch) forms a cycle",
					pluginName, dep, targetName)
			}
		}

		// 如果依赖是另一个已注册插件，递归检查
		if depPlugin, exists := pm.plugins[dep]; exists {
			if err := pm.detectCycleThroughExisting(depPlugin, targetName, batchPlugins, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// batchPluginDependsOn 检查批次中的插件是否（直接或间接）依赖目标插件
func (pm *Manager) batchPluginDependsOn(plugin *PluginDescriptor, targetName string, batchPlugins map[string]*PluginDescriptor, visited map[string]bool) bool {
	// 防止无限递归
	if visited[plugin.Name] {
		return false
	}
	visited[plugin.Name] = true

	// 检查直接依赖
	for _, dep := range plugin.Deps {
		if dep == targetName {
			return true
		}

		// 检查间接依赖（只在批次内）
		if depDesc, inBatch := batchPlugins[dep]; inBatch {
			if pm.batchPluginDependsOn(depDesc, targetName, batchPlugins, visited) {
				return true
			}
		}
	}

	return false
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

// ensureContainerInitialized 确保依赖注入容器已初始化
// 此方法应该在持有 Manager 锁的情况下调用
func (pm *Manager) ensureContainerInitialized() {
	// 创建容器（如果不存在）
	if pm.container == nil {
		pm.container = NewContainer()
	}

	// 注册已存在的插件到容器
	for pluginName, plugin := range pm.plugins {
		if !pm.container.Has(pluginName) {
			pm.container.Register(pluginName, plugin)
		}
	}

	// 注册特殊服务（只在不存在时注册，避免重复）
	if !pm.container.Has("manager") {
		pm.container.Register("manager", pm)
	}
	if !pm.container.Has("engine") {
		pm.container.Register("engine", pm.coordinator)
	}
	if !pm.container.Has("coordinator") {
		pm.container.Register("coordinator", pm.coordinator)
	}
}

// RegisterMultipleV2Smart 智能批量注册插件（自动推断依赖关系）
//
// 此方法会：
// 1. 首次尝试注册所有插件以收集依赖信息
// 2. 根据实际使用的依赖关系进行拓扑排序
// 3. 按正确顺序重新注册所有插件
//
// 优势：
//   - 不需要手动声明 Deps 字段
//   - 自动跟踪 Setup 函数中的依赖调用
//   - 自动检测循环依赖
//
// 限制：
//   - 插件的 Setup 函数必须能够多次调用而无副作用（幂等性）
//   - 或者使用 DryRun 模式（需要插件支持）
//
// 使用示例：
//
//	plugins := []*PluginDescriptor{
//	    {Name: "auth", Setup: func(ctx *SetupContext) error {
//	        // 无依赖
//	        return nil
//	    }},
//	    {Name: "permission", Setup: func(ctx *SetupContext) error {
//	        auth := ctx.MustGet("auth") // 自动检测依赖 auth
//	        return nil
//	    }},
//	}
//	// 不需要手动声明 Deps!
//	if err := manager.RegisterMultipleV2Smart(plugins); err != nil {
//	    log.Fatal(err)
//	}
func (pm *Manager) RegisterMultipleV2Smart(descriptors []*PluginDescriptor) error {
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

	// 阶段1：推断依赖关系
	logger.Info("[pluginManager] Smart registration: inferring dependencies...")

	inferredDeps := make(map[string][]string)
	descMap := make(map[string]*PluginDescriptor)

	for _, desc := range descriptors {
		descMap[desc.Name] = desc
	}

	// 创建临时容器用于依赖推断
	tempContainer := NewContainer()

	// 添加已存在的插件到临时容器
	pm.mu.RLock()
	for name, plugin := range pm.plugins {
		tempContainer.Register(name, plugin)
	}
	pm.mu.RUnlock()

	// 添加所有待注册插件的占位符到临时容器
	for _, desc := range descriptors {
		tempContainer.Register(desc.Name, &PluginInstance{desc: desc})
	}

	// 为每个插件推断依赖
	for _, desc := range descriptors {
		setupCtx := &SetupContext{
			Engine:           pm.coordinator,
			Manager:          pm,
			Config:           nil,  // 推断阶段不提供配置
			DryRun:           true, // 标记为干运行，Setup 函数应跳过有副作用的操作
			container:        tempContainer,
			pluginName:       desc.Name,
			instance:         nil,
			autoTrackEnabled: true,
		}

		// 尝试调用 Setup 来跟踪依赖（忽略错误）
		// Setup 函数应检查 ctx.DryRun == true 来跳过注册命令等副作用操作
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Setup 可能会 panic（例如 MustGet 找不到依赖）
					// 这是预期的，我们只关心 Get/MustGet 被调用了哪些插件
					logger.WithFields(logger.Fields{
						"plugin": desc.Name,
						"panic":  r,
					}).Debug("[pluginManager] Setup panicked during dependency inference (expected)")
				}
			}()

			// 忽略错误，我们只关心依赖跟踪
			_ = desc.Setup(setupCtx)
		}()

		// 获取跟踪到的依赖
		tracked := setupCtx.GetTrackedDependencies()
		if len(tracked) > 0 {
			inferredDeps[desc.Name] = tracked
			logger.WithFields(logger.Fields{
				"plugin": desc.Name,
				"deps":   tracked,
			}).Debug("[pluginManager] Inferred dependencies")
		}
	}

	// 阶段2：使用推断的依赖进行拓扑排序
	logger.Info("[pluginManager] Smart registration: sorting by dependencies...")

	// 创建带有推断依赖的描述符副本
	descriptorsWithDeps := make([]*PluginDescriptor, len(descriptors))
	for i, desc := range descriptors {
		descCopy := *desc // 浅拷贝

		// 合并声明的依赖和推断的依赖
		depsMap := make(map[string]bool)
		for _, dep := range desc.Deps {
			depsMap[dep] = true
		}
		for _, dep := range inferredDeps[desc.Name] {
			depsMap[dep] = true
		}

		mergedDeps := make([]string, 0, len(depsMap))
		for dep := range depsMap {
			mergedDeps = append(mergedDeps, dep)
		}

		descCopy.Deps = mergedDeps
		descriptorsWithDeps[i] = &descCopy
	}

	// 使用现有的 RegisterMultipleV2 进行注册
	return pm.RegisterMultipleV2(descriptorsWithDeps)
}
