package plugin

import (
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// register.go — 插件注册：DryRun 推断、拓扑排序、依赖修正

// dryRunInferDeps 通过三色标记法推断每个插件在 Setup 中实际访问的依赖。
//
// 算法（三色）：
//  1. 第一轮：为每个插件运行一次 Setup（收集 API 类型 + 依赖追踪）。
//     若依赖缺失，mustGet 在 panic 前已记录 deps。
//     每个插件最多跑一次 Setup。
//  2. 解析轮：检查所有 deps 是否已在临时容器中就绪。
//     若就绪则标记为 resolved；否则等待下一轮。
//  3. 重复步骤 2 直至无变化。剩余 unresolved 的插件形成循环依赖，返回 ErrCircularDependency。
//
// 返回 map[pluginName][]depName（含必需 + 可选依赖）。
func (pm *Manager) dryRunInferDeps(descriptors []*Descriptor) (map[string][]string, error) {
	start := time.Now()
	logger.Infof("[PluginManager] DryRun (three-color): starting dependency inference for %d plugins", len(descriptors))

	tempContainer := NewContainer()
	pm.mu.RLock()
	for name, inst := range pm.plugins {
		if api := inst.GetAPI(); api != nil {
			tempContainer.Register(name, api)
		} else {
			tempContainer.Register(name, inst)
		}
	}
	pm.mu.RUnlock()

	for _, desc := range descriptors {
		tempContainer.Register(desc.Name, &Instance{desc: desc})
	}

	// 插件状态：0=未扫描(white), 1=已扫描(grey), 2=已就绪(black)
	type dryRunPlugin struct {
		name  string
		ctx   *SetupContext
		api   any
		state int
	}
	plugins := make([]*dryRunPlugin, len(descriptors))
	for i, desc := range descriptors {
		plugins[i] = &dryRunPlugin{name: desc.Name}
	}

	// 第一轮：每个插件跑一次 Setup（收集 API + 追踪 deps）
	// 使用真实的 goroutineManager 而非 nil，使 ctx.Spawn 在 DryRun 下正常工作。
	// 插件无需判断 ctx.DryRun 即可安全使用 Spawn。
	for i, desc := range descriptors {
		gm := newGoroutineManager()
		ctx := &SetupContext{
			Reg:      &noopRegistryWriter{},
			Log:      newDryRunLogger(desc.Name),
			Info:     newPluginInfo(pm),
			EventBus: newNoopEventBus(),
			DryRun:   true,
			setupContextInternal: setupContextInternal{
				container:        tempContainer,
				pluginName:       desc.Name,
				autoTrackEnabled: true,
				goroutineMgr:     gm,
			},
		}
		var api any
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithFields(logger.Fields{
						"plugin": desc.Name,
						"panic":  r,
					}).Debug("[PluginManager] DryRun (three-color): Setup panicked")
				}
			}()
			api, _ = desc.callSetup(ctx)
		}()
		plugins[i].ctx = ctx
		plugins[i].api = api
		plugins[i].state = 1 // grey
		if api != nil {
			tempContainer.Register(desc.Name, api)
		}
	}

	// 停止所有 DryRun 阶段启动的 goroutine（Spawn 创建的 goroutine 可能仍在 runCtx.Done() 等待）
	for _, p := range plugins {
		if p.ctx != nil && p.ctx.goroutineMgr != nil {
			p.ctx.goroutineMgr.stopAndWait()
		}
	}

	// 解析轮：逐步解析 pending 类型 + 检查 deps 就绪状态
	for {
		changed := false
		for _, p := range plugins {
			if p.state == 2 {
				continue
			}
			// 尝试解析 pending type → name
			if resolved := tryResolvePendingType(p.ctx, p.name, tempContainer); resolved {
				changed = true
			}
			// 检查所有 deps 是否就绪
			allDeps := make(map[string]bool)
			for _, d := range p.ctx.getTrackedDependencies() {
				allDeps[d] = true
			}
			for _, d := range p.ctx.getTrackedOptionalDependencies() {
				allDeps[d] = true
			}
			resolved := true
			for dep := range allDeps {
				if dep == p.name {
					continue
				}
				if _, ok := tempContainer.Get(dep); !ok {
					resolved = false
					break
				}
			}
			if resolved {
				p.state = 2
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// 收集推断结果（所有 resolved 的 dep）
	inferred := make(map[string][]string)
	for _, p := range plugins {
		if p.state != 2 {
			// 经过多轮仍未就绪 → 循环依赖
			logger.WithField("plugin", p.name).Warn("[PluginManager] DryRun (three-color): unresolved dependency (possible circular)")
			continue
		}
		allTracked := make(map[string]bool)
		for _, d := range p.ctx.getTrackedDependencies() {
			allTracked[d] = true
		}
		for _, d := range p.ctx.getTrackedOptionalDependencies() {
			allTracked[d] = true
		}
		if len(allTracked) > 0 {
			tracked := make([]string, 0, len(allTracked))
			for d := range allTracked {
				tracked = append(tracked, d)
			}
			inferred[p.name] = tracked
		}
	}

	// 检查是否有 unresolved 的插件 → 循环依赖
	unresolved := make([]string, 0)
	for _, p := range plugins {
		if p.state != 2 {
			unresolved = append(unresolved, p.name)
		}
	}
	if len(unresolved) > 0 {
		err := fmt.Errorf("%w: %v", errutil.ErrCircularDependency, unresolved)
		logger.WithError(err).Errorf("[PluginManager] DryRun (three-color): %d plugin(s) unresolved after %v", len(unresolved), time.Since(start))
		return inferred, err
	}
	logger.Infof("[PluginManager] DryRun (three-color): completed in %v — %d resolved, %d total",
		time.Since(start), len(descriptors)-len(unresolved), len(descriptors))
	return inferred, nil
}

// tryResolvePendingType 在容器中查找与 setupCtx.pendingType 匹配的已注册 API。
// 找到后将 dep 名称追加到 trackedDeps，使依赖能被 topologicalSort 感知。
func tryResolvePendingType(setupCtx *SetupContext, pluginName string, c *Container) bool {
	if setupCtx.pendingType == nil {
		return false
	}
	entries := lookupServiceTypeByReflect(c, setupCtx.pendingType)
	if len(entries) == 0 {
		return false
	}
	if len(entries) > 1 {
		logger.Warnf("[PluginManager] DryRun: pending type %v resolved to multiple services: %v, picking first", setupCtx.pendingType, entryNames(entries))
	}
	name := entries[0].name
	if setupCtx.trackedDeps == nil {
		setupCtx.trackedDeps = make(map[string]bool)
	}
	setupCtx.trackedDeps[name] = true
	logger.Debugf("[PluginManager] DryRun: pending type %v resolved -> %q for plugin %q", setupCtx.pendingType, name, pluginName)
	return true
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
		insertAt := min(instPos, len(newOrder))
		newOrder = append(newOrder[:insertAt], append([]string{p.dep}, newOrder[insertAt:]...)...)
		pm.loadOrder = newOrder

		// 更新位置索引
		for i, name := range pm.loadOrder {
			pos[name] = i
		}
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
		return nil, fmt.Errorf("circular dependency detected among plugins: %v: %w", unprocessed, errutil.ErrCircularDependency)
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
