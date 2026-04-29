package plugin

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// register.go — 插件注册：Register、批量注册、拓扑排序

// Register 注册插件（使用 Descriptor）
//
// 锁策略（三段式，最小化锁持有时间）：
//
//	Lock #1: 重复检查 + 依赖校验 + 版本约束
//	(解锁)  ←  I/O：NewPluginConfigFromProvider / validateConfigSchema
//	Lock #2: 再次重复检查 + pm.plugins[name] = instance
//	(解锁)
//	instance.load()  ← 已在锁外
//	Lock #3: 错误清理 / 依赖合并 / loadOrder / container.Register
func (pm *Manager) Register(desc *Descriptor) error {
	if err := validateDescriptor(desc); err != nil {
		return err
	}

	name := desc.Name

	// ========== Lock #1: 快速校验 ==========
	pm.mu.Lock()

	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		logger.Warnf("[PluginManager] Plugin %s already registered", name)
		return errutil.ErrPluginAlreadyExists
	}

	registeredList := func() []string {
		names := make([]string, 0, len(pm.plugins))
		for n := range pm.plugins {
			names = append(names, n)
		}
		return names
	}

	if err := checkDependencies(pm, desc, registeredList); err != nil {
		pm.mu.Unlock()
		return err
	}

	if err := validateVersionConstraints(pm, desc); err != nil {
		pm.mu.Unlock()
		return err
	}

	pm.ensureContainerInitialized()

	// 在锁内读取 configProvider，移出锁外调用（I/O 可能阻塞）
	cp := pm.configProvider

	// Reload/Strategy 检测（仅读 desc，不需要锁，但已在锁内顺手完成）
	if desc.Advanced != nil && desc.Advanced.Reload != nil && desc.Advanced.Strategy != ReloadInPlace {
		pm.mu.Unlock()
		return fmt.Errorf("plugin %q: Advanced.Reload is set but Strategy is %v (not ReloadInPlace). "+
			"The Reload func will NOT be called with this strategy. Did you mean Strategy: plugin.ReloadInPlace?", name, desc.Advanced.Strategy)
	}

	pm.mu.Unlock()
	// ========== Lock #1 结束 ==========

	// ========== 无锁区：Config 构造（可能 I/O）+ Schema 校验 ==========
	var config Config
	if cp != nil {
		config = NewPluginConfigFromProvider(name, cp)
	}

	if err := validateConfigSchema(name, desc, config); err != nil {
		return err
	}

	// ========== 无锁区：实例 + SetupContext 构建（纯内存操作）==========
	instance := &Instance{
		desc:     desc,
		state:    Unloaded,
		matchers: make([]*engine.Matcher, 0),
	}

	var adminView ManagerWriter
	if desc.Privileged {
		adminView = newManagerWriter(pm)
	}

	setupCtx := &SetupContext{
		Reg:      newLiveRegistryWriter(pm.coordinator, name, instance),
		Log:      newPluginLogger(name),
		Info:     newPluginInfo(pm),
		Admin:    adminView,
		Config:   config,
		EventBus: pm.eventBus,
		setupContextInternal: setupContextInternal{
			container:        pm.container,
			pluginName:       name,
			instance:         instance,
			autoTrackEnabled: true,
			eng:              pm.coordinator,
		},
	}

	instance.setupContext = setupCtx
	instance.state = Loading

	// ========== Lock #2: 存入 plugins 表（短）==========
	pm.mu.Lock()
	// 二次重复检查：Lock#1~Lock#2 窗口期可能已被其他 Register 抢占注册同名插件
	if _, exists := pm.plugins[name]; exists {
		pm.mu.Unlock()
		return errutil.ErrPluginAlreadyExists
	}
	pm.plugins[name] = instance
	pm.mu.Unlock()
	// ========== Lock #2 结束 ==========

	// ========== 无锁区：执行 Setup ==========
	loadErr := instance.load()

	// ========== Lock #3: 最终化 ==========
	pm.mu.Lock()

	if loadErr != nil {
		delete(pm.plugins, name)
		pm.container.Remove(name)
		pm.mu.Unlock()
		logger.WithError(loadErr).Errorf("[PluginManager] Failed to load plugin %s", name)
		pm.notifyError(name, "load", loadErr)
		return loadErr
	}

	trackedDeps := setupCtx.GetTrackedDependencies()
	trackedOptional := setupCtx.GetTrackedOptionalDependencies()

	allTracked := make(map[string]bool, len(trackedDeps)+len(trackedOptional))
	for _, d := range trackedDeps {
		allTracked[d] = true
	}
	for _, d := range trackedOptional {
		allTracked[d] = true
	}

	if len(allTracked) > 0 {
		declaredDeps := make(map[string]bool)
		for _, dep := range desc.Deps {
			declaredDeps[dep] = true
		}
		for _, dep := range desc.OptionalDeps {
			declaredDeps[dep] = true
		}

		undeclaredAll := make([]string, 0)
		for dep := range allTracked {
			if !declaredDeps[dep] {
				undeclaredAll = append(undeclaredAll, dep)
			}
		}

		if len(undeclaredAll) > 0 {
			if pm.strictDeps {
				pm.mu.Unlock()
				if teardownErr := instance.unload(pm.coordinator); teardownErr != nil {
					logger.WithError(teardownErr).Warnf("[PluginManager] Failed to teardown plugin %s during strict-mode rollback", name)
				}
				pm.mu.Lock()
				delete(pm.plugins, name)
				pm.container.Remove(name)
				pm.mu.Unlock()
				return fmt.Errorf(
					"plugin %q uses undeclared dependencies %v (declared: %v); "+
						"add them to Deps or disable strict mode via manager.SetStrictDeps(false)",
					name, undeclaredAll, desc.Deps,
				)
			}
			logger.WithFields(logger.Fields{
				"plugin":          name,
				"undeclared_deps": undeclaredAll,
				"declared_deps":   desc.Deps,
			}).Warn("[PluginManager] Plugin uses dependencies not declared in Deps field")
		}

		var undeclaredRequired []string
		for _, d := range trackedDeps {
			if !declaredDeps[d] {
				undeclaredRequired = append(undeclaredRequired, d)
			}
		}
		if len(undeclaredRequired) > 0 {
			mergedDeps := make([]string, len(desc.Deps), len(desc.Deps)+len(undeclaredRequired))
			copy(mergedDeps, desc.Deps)
			mergedDeps = append(mergedDeps, undeclaredRequired...)
			newDesc := *desc
			newDesc.Deps = mergedDeps
			instance.desc = &newDesc
			instance.depsModified = true
		}
	}

	pm.loadOrder = append(pm.loadOrder, name)

	if !pm.container.Has(name) {
		pm.container.Register(name, instance)
	}

	pm.mu.Unlock()
	// ========== Lock #3 结束 ==========

	logger.Infof("[PluginManager] Plugin %s registered (v2)", name)
	pm.notifyLoaded(name)
	return nil
}

// RegisterMultiple 批量注册多个 v2 插件，自动处理依赖顺序。
//
// 拓扑排序基于声明 Deps + OptionalDeps。Setup 中发现的未声明依赖（通过 COW 合并）
// 会在所有插件注册完成后由 rectifyLoadOrder 修正 loadOrder（不影响 Setup 顺序）。
//
// 若需要完全基于运行时依赖推断排序（要求 Setup 幂等），请使用 RegisterMultipleSmart。
//
// 任意插件注册失败时，已注册的插件不会自动回滚（使用 RegisterMultipleAtomic 获得原子保证）。
func (pm *Manager) RegisterMultiple(descriptors []*Descriptor) error {
	if len(descriptors) == 0 {
		return nil
	}
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
	sorted, err := pm.topologicalSort(descriptors)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}
	registered := make([]*Instance, 0, len(sorted))
	for _, desc := range sorted {
		if err := pm.Register(desc); err != nil {
			return fmt.Errorf("failed to register plugin %s: %w", desc.Name, err)
		}
		if inst, ok := pm.Get(desc.Name); ok {
			registered = append(registered, inst)
		}
	}
	pm.rectifyLoadOrder(registered)
	logger.Infof("[PluginManager] Successfully registered %d plugins in dependency order", len(sorted))
	return nil
}

// RegisterMultipleAtomic 原子批量注册：任意插件失败时，自动逆序回滚已注册的插件。
func (pm *Manager) RegisterMultipleAtomic(descriptors []*Descriptor) error {
	if len(descriptors) == 0 {
		return nil
	}
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
	sorted, err := pm.topologicalSort(descriptors)
	if err != nil {
		return &PluginError{
			Operation: "batch register",
			Cause:     err,
			Hint:      "check for circular or missing dependencies",
		}
	}
	registered := make([]string, 0, len(sorted))
	registeredInsts := make([]*Instance, 0, len(sorted))
	for _, desc := range sorted {
		if err := pm.Register(desc); err != nil {
			for i := len(registered) - 1; i >= 0; i-- {
				if rollbackErr := pm.Unregister(registered[i]); rollbackErr != nil {
					logger.WithError(rollbackErr).Warnf("[PluginManager] Rollback failed for plugin %s", registered[i])
				}
			}
			pm.mu.RLock()
			existingNames := make([]string, 0, len(pm.plugins))
			for n := range pm.plugins {
				existingNames = append(existingNames, n)
			}
			pm.mu.RUnlock()
			return &PluginError{
				PluginName:        desc.Name,
				Operation:         "register",
				Cause:             err,
				RegisteredPlugins: existingNames,
				Hint:              "all previously registered plugins in this batch have been rolled back",
			}
		}
		registered = append(registered, desc.Name)
		if inst, ok := pm.Get(desc.Name); ok {
			registeredInsts = append(registeredInsts, inst)
		}
	}
	pm.rectifyLoadOrder(registeredInsts)
	logger.Infof("[PluginManager] Atomic registration of %d plugins succeeded", len(sorted))
	return nil
}

// RegisterMultipleSmart 智能批量注册：自动推断依赖关系（无需手动声明 Deps）。
//
// 限制：插件的 Setup 函数必须幂等（能安全多次调用而无副作用）。
//
// 实现：先 DryRun 推断实际访问的依赖，合并到 Deps 后再执行拓扑排序 + 注册 + 事后修正。
func (pm *Manager) RegisterMultipleSmart(descriptors []*Descriptor) error {
	if len(descriptors) == 0 {
		return nil
	}
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

	logger.Info("[PluginManager] Smart registration: inferring dependencies...")

	inferred := pm.dryRunInferDeps(descriptors)
	enriched := mergeInferredDeps(descriptors, inferred)

	sorted, err := pm.topologicalSort(enriched)
	if err != nil {
		return fmt.Errorf("dependency resolution failed: %w", err)
	}
	registered := make([]*Instance, 0, len(sorted))
	for _, desc := range sorted {
		if err := pm.Register(desc); err != nil {
			return fmt.Errorf("failed to register plugin %s: %w", desc.Name, err)
		}
		if inst, ok := pm.Get(desc.Name); ok {
			registered = append(registered, inst)
		}
	}
	pm.rectifyLoadOrder(registered)
	logger.Infof("[PluginManager] Smart registration of %d plugins succeeded", len(sorted))
	return nil
}

// dryRunInferDeps 对一组 descriptor 执行 DryRun，推断每个插件实际访问的依赖。
// 返回 map[pluginName][]depName（含必需 + 可选依赖）。
// Setup 中的 panic 会被 recover，不影响其他插件的推断。
func (pm *Manager) dryRunInferDeps(descriptors []*Descriptor) map[string][]string {
	descMap := make(map[string]*Descriptor, len(descriptors))
	for _, desc := range descriptors {
		descMap[desc.Name] = desc
	}

	// 创建临时容器：复制已注册的插件 + 本次批次的全部插件
	tempContainer := NewContainer()
	pm.mu.RLock()
	for name, plugin := range pm.plugins {
		tempContainer.Register(name, plugin)
	}
	pm.mu.RUnlock()
	for _, desc := range descriptors {
		tempContainer.Register(desc.Name, &Instance{desc: desc})
	}

	inferred := make(map[string][]string)
	for _, desc := range descriptors {
		setupCtx := &SetupContext{
			Reg:      &noopRegistryWriter{},
			Log:      newPluginLogger(desc.Name),
			Info:     newPluginInfo(pm),
			EventBus: newNoopEventBus(),
			DryRun:   true,
			setupContextInternal: setupContextInternal{
				container:        tempContainer,
				pluginName:       desc.Name,
				autoTrackEnabled: true,
			},
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithFields(logger.Fields{
						"plugin": desc.Name,
						"panic":  r,
					}).Debug("[PluginManager] DryRun: Setup panicked during dependency inference")
				}
			}()
			_, _ = desc.callSetup(setupCtx)
		}()
		allTracked := make(map[string]bool)
		for _, d := range setupCtx.GetTrackedDependencies() {
			allTracked[d] = true
		}
		for _, d := range setupCtx.GetTrackedOptionalDependencies() {
			allTracked[d] = true
		}
		if len(allTracked) > 0 {
			tracked := make([]string, 0, len(allTracked))
			for d := range allTracked {
				tracked = append(tracked, d)
			}
			inferred[desc.Name] = tracked
		}
	}
	return inferred
}

// mergeInferredDeps 将 DryRun 推断出的依赖合并到 descriptor 副本中，返回新的切片。
//
// 合并规则：
//   - 推断出的依赖若尚未在 Deps / OptionalDeps 中，则追加到 Deps
//   - 若推断出的依赖原本仅在 OptionalDeps 中声明，则从 OptionalDeps 移除
//     （它已升级为必需 dep，防止拓扑排序时双重计数）
func mergeInferredDeps(descriptors []*Descriptor, inferred map[string][]string) []*Descriptor {
	result := make([]*Descriptor, len(descriptors))
	for i, desc := range descriptors {
		tracked, hasInferred := inferred[desc.Name]
		if !hasInferred {
			result[i] = desc
			continue
		}

		// 构建声明集合（Deps + OptionalDeps）
		declaredSet := make(map[string]bool, len(desc.Deps)+len(desc.OptionalDeps))
		for _, dep := range desc.Deps {
			declaredSet[dep] = true
		}
		for _, dep := range desc.OptionalDeps {
			declaredSet[dep] = true
		}

		// 找出真正新增的 dep
		var newDeps []string
		for _, dep := range tracked {
			if !declaredSet[dep] {
				newDeps = append(newDeps, dep)
			}
		}
		if len(newDeps) == 0 {
			result[i] = desc
			continue
		}

		// 合并到 Deps（去重），同时从 OptionalDeps 中移除已升级的
		depsSet := make(map[string]bool, len(desc.Deps)+len(newDeps))
		for _, dep := range desc.Deps {
			depsSet[dep] = true
		}
		for _, dep := range newDeps {
			depsSet[dep] = true
		}
		mergedDeps := make([]string, 0, len(depsSet))
		for dep := range depsSet {
			mergedDeps = append(mergedDeps, dep)
		}

		// 从 OptionalDeps 中移除已从推断升级的 dep
		newOptional := make([]string, 0, len(desc.OptionalDeps))
		for _, dep := range desc.OptionalDeps {
			if !depsSet[dep] {
				newOptional = append(newOptional, dep)
			}
		}

		newDesc := *desc
		newDesc.Deps = mergedDeps
		newDesc.OptionalDeps = newOptional
		result[i] = &newDesc
	}
	return result
}

// rectifyLoadOrder 对本次注册的实例，检查其合并的 dep 是否在 loadOrder 中位于自身之后。
// 若是，移动 dep 到 dependent 之前，确保 loadOrder 满足合并后的依赖关系。
//
// 通常在 Register 阶段已通过拓扑排序保证了顺序正确；此方法作为兜底，
// 处理边界情况（如 DryRun 未捕获、单个 Register 发生的 COW 合并等）。
func (pm *Manager) rectifyLoadOrder(instances []*Instance) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	type pair struct{ dep, dependent string }
	var needFix []pair

	for _, inst := range instances {
		if !inst.depsModified {
			continue
		}
		for _, dep := range inst.desc.Deps {
			needFix = append(needFix, pair{dep: dep, dependent: inst.Name()})
		}
	}
	if len(needFix) == 0 {
		return
	}

	// 构建 loadOrder 位置索引
	pos := make(map[string]int, len(pm.loadOrder))
	for i, name := range pm.loadOrder {
		pos[name] = i
	}

	for _, p := range needFix {
		depPos, depOk := pos[p.dep]
		instPos, instOk := pos[p.dependent]
		if !depOk || !instOk {
			continue
		}
		if depPos < instPos {
			continue // dep 已在 dependent 之前，正确
		}
		// dep 在 dependent 之后 → 将 dep 移动到 dependent 前一个位置
		logger.WithFields(logger.Fields{
			"dep":       p.dep,
			"dependent": p.dependent,
		}).Warn("[PluginManager] Fixing load order: moving dependency before dependent")

		// 从 loadOrder 中移除 dep
		newOrder := make([]string, 0, len(pm.loadOrder))
		for _, name := range pm.loadOrder {
			if name != p.dep {
				newOrder = append(newOrder, name)
			}
		}
		// 在 dependent 前面插入 dep
		insertAt := instPos
		if insertAt > len(newOrder) {
			insertAt = len(newOrder)
		}
		newOrder = append(newOrder[:insertAt], append([]string{p.dep}, newOrder[insertAt:]...)...)
		pm.loadOrder = newOrder

		// 更新位置索引
		for i, name := range pm.loadOrder {
			pos[name] = i
		}
	}
}

// ValidateDependencies 验证一组插件的依赖关系（不注册）
func (pm *Manager) ValidateDependencies(descriptors []*Descriptor) error {
	_, err := pm.topologicalSort(descriptors)
	return err
}

// ensureContainerInitialized 确保依赖注入容器已初始化（须在持有 Manager 锁时调用）
func (pm *Manager) ensureContainerInitialized() {
	if pm.container == nil {
		pm.container = NewContainer()
	}
	for pluginName, plugin := range pm.plugins {
		if !pm.container.Has(pluginName) {
			pm.container.Register(pluginName, plugin)
		}
	}
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

// topologicalSort 使用 Kahn 算法进行拓扑排序，检测循环依赖
func (pm *Manager) topologicalSort(descriptors []*Descriptor) ([]*Descriptor, error) {
	descMap := make(map[string]*Descriptor)
	for _, desc := range descriptors {
		if _, exists := descMap[desc.Name]; exists {
			return nil, fmt.Errorf("duplicate plugin name: %s", desc.Name)
		}
		descMap[desc.Name] = desc
	}

	if err := pm.checkCrossBatchCyclicDependency(descriptors, descMap); err != nil {
		return nil, err
	}

	inDegree := make(map[string]int)
	graph := make(map[string][]string)
	for name := range descMap {
		inDegree[name] = 0
		graph[name] = make([]string, 0)
	}
	for _, desc := range descriptors {
		for _, dep := range desc.Deps {
			pm.mu.RLock()
			depInst, existsInManager := pm.plugins[dep]
			pm.mu.RUnlock()
			_, existsInBatch := descMap[dep]
			if !existsInManager && !existsInBatch {
				return nil, fmt.Errorf("plugin %s has missing dependency: %s", desc.Name, dep)
			}
			if existsInManager && !existsInBatch {
				if depInst.GetState() != Loaded {
					return nil, fmt.Errorf("plugin %s dependency '%s' is not ready (state: %s)", desc.Name, dep, depInst.GetState())
				}
			}
			if existsInBatch {
				inDegree[desc.Name]++
				graph[dep] = append(graph[dep], desc.Name)
			}
		}
		// OptionalDeps：仅当可选依赖存在于同一批次时才参与拓扑排序，不存在则静默跳过
		for _, dep := range desc.OptionalDeps {
			if _, existsInBatch := descMap[dep]; existsInBatch {
				inDegree[desc.Name]++
				graph[dep] = append(graph[dep], desc.Name)
			}
		}
	}

	queue := make([]string, 0)
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	result := make([]*Descriptor, 0, len(descriptors))
	processed := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, descMap[current])
		processed++
		for _, dependent := range graph[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if processed != len(descriptors) {
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
func (pm *Manager) checkCrossBatchCyclicDependency(descriptors []*Descriptor, descMap map[string]*Descriptor) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, desc := range descriptors {
		for _, depName := range desc.Deps {
			existingInst, existsInManager := pm.plugins[depName]
			if !existsInManager {
				continue
			}
			if err := pm.detectCycleThroughExisting(existingInst, desc.Name, descMap, make(map[string]bool)); err != nil {
				return fmt.Errorf("cross-batch circular dependency: %w", err)
			}
		}
	}
	return nil
}

func (pm *Manager) detectCycleThroughExisting(existingInst *Instance, targetName string, batchPlugins map[string]*Descriptor, visited map[string]bool) error {
	pluginName := existingInst.Name()
	if visited[pluginName] {
		return nil
	}
	visited[pluginName] = true
	for _, dep := range existingInst.dependencies() {
		if dep == targetName {
			return fmt.Errorf("plugin %s (registered) depends on %s (in batch), which depends on %s", pluginName, dep, pluginName)
		}
		if batchDesc, inBatch := batchPlugins[dep]; inBatch {
			if pm.batchPluginDependsOn(batchDesc, targetName, batchPlugins, make(map[string]bool)) {
				return fmt.Errorf("plugin %s (registered) -> %s (batch) -> %s (batch) forms a cycle", pluginName, dep, targetName)
			}
		}
		if depInst, exists := pm.plugins[dep]; exists {
			if err := pm.detectCycleThroughExisting(depInst, targetName, batchPlugins, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

func (pm *Manager) batchPluginDependsOn(plugin *Descriptor, targetName string, batchPlugins map[string]*Descriptor, visited map[string]bool) bool {
	if visited[plugin.Name] {
		return false
	}
	visited[plugin.Name] = true
	for _, dep := range plugin.Deps {
		if dep == targetName {
			return true
		}
		if depDesc, inBatch := batchPlugins[dep]; inBatch {
			if pm.batchPluginDependsOn(depDesc, targetName, batchPlugins, visited) {
				return true
			}
		}
	}
	return false
}
