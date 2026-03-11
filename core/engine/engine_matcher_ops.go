package engine

// engine_matcher_ops.go — Matcher 注册、删除、分组、迁移等写操作
//
// 本文件包含所有与 Matcher 生命周期相关的写操作：
//   - 注册（On/OnC2C/OnGroupAt/OnTemp/BatchRegisterMatchers）
//   - 删除（DeleteMatcher/DeleteMatchers/DeleteAllMatchers）
//   - 分组（SetMatcherGroup/WithMatcherGroupBatch/RemoveGroup）
//   - 状态修改（SetBlock/SetMaxMatchers/InvalidateSortedCache）
//   - 迁移（MigrateMatcherToTemp/FromTemp）
//   - 索引维护（UpdateMatcherIndex/UpdateMatcherCommand/UpdateCommandCache）

import (
	"strings"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// noopMatcher 是一个空操作匹配器，用于在达到匹配器限制时返回。
// 所有方法都返回自身，形成无操作链。
var noopMatcher = &Matcher{
	rt:          matcherRuntime{deleted: true},
	priority:    999,
	Source:      "noop",
	Rules:       []context.Rule{},
	middlewares: []Middleware{},
}

// newNoopMatcher 创建一个绑定了 coordinator 的 noop matcher。
func newNoopMatcher(e *Engine) *Matcher {
	return &Matcher{
		rt:          matcherRuntime{deleted: true},
		priority:    999,
		Source:      "noop",
		Rules:       []context.Rule{},
		middlewares: []Middleware{},
		coordinator: e,
	}
}

// ---- 删除操作 ----------------------------------------------------------------

// DeleteAllMatchers 删除引擎中的所有匹配器（COW 写操作）
func (e *Engine) DeleteAllMatchers() {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	oldMatchers := append([]*Matcher(nil), oldState.matchers...)

	newState := copyEngineState(oldState)
	newState.matchers = make([]*Matcher, 0)
	newState.matcherIndex = make(map[EventType][]*Matcher)
	newState.sortedCache = make(map[EventType][]*Matcher)

	e.state.Store(newState)

	for _, m := range oldMatchers {
		if m == nil {
			continue
		}
		m.rt.mu.Lock()
		m.rt.deleted = true
		m.rt.mu.Unlock()
	}
}

// DeleteMatcher 删除指定的匹配器（COW 写操作）
func (e *Engine) DeleteMatcher(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.deleteMatcher(m)
	e.state.Store(newState)
}

// DeleteMatchers 批量删除匹配器（COW 写操作）
func (e *Engine) DeleteMatchers(matchers []*Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	state := e.state.Load()
	newState := copyEngineState(state)
	newState.deleteMatchers(matchers)
	e.state.Store(newState)
}

// ---- 注册操作 ----------------------------------------------------------------

// registerMatcher 注册一个已初始化的匹配器（内部方法，COW 写操作）
func (e *Engine) registerMatcher(m *Matcher) *Matcher {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()

	if oldState.maxMatchers > 0 && len(oldState.matchers) >= oldState.maxMatchers {
		logger.Errorf("[engine] Matcher limit reached: %d/%d, returning noop matcher",
			len(oldState.matchers), oldState.maxMatchers)
		return newNoopMatcher(e)
	}

	newState := copyEngineState(oldState)
	newState.addMatcher(m)
	e.state.Store(newState)

	e.rebuildMatcherChainCOW(m)
	return m
}

// BatchRegisterMatchers 批量注册多个匹配器（COW 写操作）
//
// 相比多次调用 registerMatcher，此方法只执行一次 COW 复制，
// 在批量注册场景下可以大幅提升性能（3-5x）。
//
// 使用场景：
//   - 插件初始化时注册多个匹配器
//   - 配置热更新时重新注册所有匹配器
func (e *Engine) BatchRegisterMatchers(matchers []*Matcher) []*Matcher {
	if len(matchers) == 0 {
		return matchers
	}

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()

	newCount := len(oldState.matchers) + len(matchers)
	if oldState.maxMatchers > 0 && newCount > oldState.maxMatchers {
		logger.Errorf("[engine] Matcher limit reached: %d+%d > %d, truncating batch",
			len(oldState.matchers), len(matchers), oldState.maxMatchers)

		available := oldState.maxMatchers - len(oldState.matchers)
		if available <= 0 {
			noop := make([]*Matcher, len(matchers))
			for i := range noop {
				noop[i] = newNoopMatcher(e)
			}
			return noop
		}
		matchers = matchers[:available]
	}

	newState := copyEngineState(oldState)
	for _, m := range matchers {
		newState.addMatcher(m)
	}
	e.state.Store(newState)

	for _, m := range matchers {
		e.rebuildMatcherChainCOW(m)
	}

	logger.Debugf("[engine] Batch registered %d matchers", len(matchers))
	return matchers
}

// ---- 事件类型便捷注册 ---------------------------------------------------------

// On 注册一个新的事件匹配器，显式指定事件类型（COW 写操作）
func (e *Engine) On(eventType EventType, rules ...context.Rule) *Matcher {
	matcher := &Matcher{
		EventType:   eventType,
		Rules:       rules,
		coordinator: e,
		priority:    50,
		Source:      "global",
	}
	return e.registerMatcher(matcher)
}

// OnAny 注册一个适用于所有事件类型的匹配器
func (e *Engine) OnAny(rules ...context.Rule) *Matcher {
	return e.On("", rules...)
}

// OnC2C 是 On(dto.C2CMessageCreate, ...) 的便捷封装
func (e *Engine) OnC2C(rules ...context.Rule) *Matcher {
	return e.On(dto.C2CMessageCreate, rules...)
}

// OnGroupAt 是 On(dto.GroupAtMessageCreate, ...) 的便捷封装
func (e *Engine) OnGroupAt(rules ...context.Rule) *Matcher {
	return e.On(dto.GroupAtMessageCreate, rules...)
}

// OnGroupAdd 是 On(dto.GroupAddRobot, ...) 的便捷封装
func (e *Engine) OnGroupAdd(rules ...context.Rule) *Matcher {
	return e.On(dto.GroupAddRobot, rules...)
}

// OnGroupDel 是 On(dto.GroupDelRobot, ...) 的便捷封装
func (e *Engine) OnGroupDel(rules ...context.Rule) *Matcher {
	return e.On(dto.GroupDelRobot, rules...)
}

// OnFullMatch 注册一个完全匹配器
func (e *Engine) OnFullMatch(text string, extraRules ...context.Rule) *Matcher {
	finalRules := append([]context.Rule{context.OnFullMatch(text)}, extraRules...)
	return e.OnAny(finalRules...)
}

// OnTemp 注册一个临时的事件匹配器（非 COW 模式）
//
// 专为高频创建/销毁的场景优化，避免 COW 的全量复制开销。
// 临时 Matcher 默认为一次性（使用 1 次后删除）。
func (e *Engine) OnTemp(eventType EventType, rules ...context.Rule) *Matcher {
	matcher := &Matcher{
		EventType:   eventType,
		Rules:       rules,
		coordinator: e,
		priority:    50,
		Source:      "temp",
		rt: matcherRuntime{
			isTemp:      1,
			maxUseCount: 1,
		},
	}

	e.services.tempManager.Add(matcher)
	e.rebuildMatcherChainCOW(matcher)
	return matcher
}

// ---- 状态修改操作 -------------------------------------------------------------

// InvalidateSortedCache 失效指定事件类型的排序缓存（COW 写操作）
func (e *Engine) InvalidateSortedCache(eventType EventType) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.invalidateSortedCache(eventType)
	e.state.Store(newState)
}

// SetBlock 设置引擎的阻塞状态（COW 写操作）
func (e *Engine) SetBlock(block bool) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.block = block
	e.state.Store(newState)
	return e
}

// SetMaxMatchers 设置匹配器数量上限（COW 写操作）
// 设置为 0 表示不限制（默认）
func (e *Engine) SetMaxMatchers(limit int) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.maxMatchers = limit
	e.state.Store(newState)
	return e
}

// EnableGlobalMatchers 启用/禁用全局匹配器
func (e *Engine) EnableGlobalMatchers(enable bool) {
	e.SetBlock(!enable)
}

// ---- 分组操作 ----------------------------------------------------------------

// RemoveGroup 根据分组名称删除所有匹配器（COW 写操作）
func (e *Engine) RemoveGroup(groupName string) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	state := e.state.Load()
	if _, ok := state.groupIndex[groupName]; !ok {
		return
	}

	newState := copyEngineState(state)
	newState.removeGroup(groupName)
	e.state.Store(newState)

	logger.Debugf("[engine] Removed matcher group: %s", groupName)
}

// DisableGroup 暂停指定分组的所有 Matcher 响应。
//
// 与 RemoveGroup 的区别：
//   - RemoveGroup：永久删除，无法恢复
//   - DisableGroup：仅标记为禁用，可通过 EnableGroup 恢复
func (e *Engine) DisableGroup(groupName string) {
	if groupName == "" {
		return
	}
	state := e.state.Load()
	matchers, ok := state.groupIndex[groupName]
	if !ok {
		return
	}
	for _, m := range matchers {
		m.disable()
	}
	logger.Debugf("[engine] Disabled matcher group: %s (%d matchers)", groupName, len(matchers))
}

// EnableGroup 恢复指定分组的所有 Matcher 响应。
func (e *Engine) EnableGroup(groupName string) {
	if groupName == "" {
		return
	}
	state := e.state.Load()
	matchers, ok := state.groupIndex[groupName]
	if !ok {
		return
	}
	for _, m := range matchers {
		m.enable()
	}
	logger.Debugf("[engine] Enabled matcher group: %s (%d matchers)", groupName, len(matchers))
}

// WithMatcherGroupBatch 在单次 COW 写操作中提交多个 group 更新。
//
// 为什么需要：插件加载时会创建大量 Matcher 并统一划入同一 group，
// 若逐个更新索引会造成写放大。此方法保证 fn 执行完成后仅做一次索引重建。
//
// 注意：fn 内部仍可以调用 registerMatcher 等持有 writeMu 的方法，
// 因为本方法不在 fn 执行期间持有 writeMu。
func (e *Engine) WithMatcherGroupBatch(fn func()) {
	if fn == nil {
		return
	}
	fn()

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.rebuildIndex()
	e.state.Store(newState)
}

// SetMatcherGroup 将已注册的 matcher 划入指定 group 并更新来源标签。
// 这是将 matcher 关联到插件分组的首选方法。
func (e *Engine) SetMatcherGroup(m *Matcher, group, source string) {
	if m == nil {
		return
	}

	oldGroup := ""
	m.rt.mu.Lock()
	oldGroup = m.group
	m.group = strings.TrimSpace(group)
	if source != "" {
		m.Source = source
	}
	m.invalidateCombinedChain()
	m.rt.mu.Unlock()

	if oldGroup != m.group {
		e.writeMu.Lock()
		oldState := e.state.Load()
		newState := copyEngineState(oldState)

		if oldGroup != "" {
			filtered := make([]*Matcher, 0, len(newState.groupIndex[oldGroup]))
			for _, gm := range newState.groupIndex[oldGroup] {
				if gm != m {
					filtered = append(filtered, gm)
				}
			}
			if len(filtered) == 0 {
				delete(newState.groupIndex, oldGroup)
			} else {
				newState.groupIndex[oldGroup] = filtered
			}
		}

		newGroupName := strings.TrimSpace(group)
		if newGroupName != "" {
			newState.groupIndex[newGroupName] = append(newState.groupIndex[newGroupName], m)
		}

		e.state.Store(newState)
		e.writeMu.Unlock()
	}

	e.rebuildMatcherChainCOW(m)
}

// ---- 迁移操作 ----------------------------------------------------------------

// UpdateTempMatcherPriority 更新临时 matcher 的优先级（内部方法）
func (e *Engine) UpdateTempMatcherPriority(m *Matcher) {
	e.services.tempManager.Remove(m)
	e.services.tempManager.Add(m)
}

// MigrateMatcherToTemp 将 matcher 迁移到 TempManager
func (e *Engine) MigrateMatcherToTemp(m *Matcher) {
	e.services.tempManager.Add(m)
	e.removeMatcherFromStateSilently(m)
}

// MigrateMatcherFromTemp 将 matcher 从 TempManager 迁移到 State
func (e *Engine) MigrateMatcherFromTemp(m *Matcher) {
	e.addMatcherToStateSilently(m)
	e.services.tempManager.Remove(m)
}

// ---- 索引维护 ----------------------------------------------------------------

// UpdateMatcherCommand 重新索引指定的 matcher（COW 写操作）
// 当 matcher 的 command 属性变化时调用
func (e *Engine) UpdateMatcherCommand(m *Matcher) {
	e.UpdateMatcherIndex(m)
}

// UpdateMatcherIndex 强制更新匹配器的索引（COW 写操作）。
//
// 仅重建受影响 matcher 的 EventType 对应的 sortedCache，
// 避免每次都全量重建所有索引（原实现 O(N) 全量重建）。
// 若 m 为 nil，则回退到全量重建。
func (e *Engine) UpdateMatcherIndex(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)

	if m == nil {
		newState.rebuildIndex()
		e.state.Store(newState)
		return
	}

	cmd := m.GetCommand()
	et := m.EventType

	if cmd != "" {
		if cmdMap, ok := newState.commandIndex[cmd]; ok {
			if matchers, ok := cmdMap[et]; ok {
				sortMatchersByPriority(matchers)
			}
		}
	} else {
		if matchers, ok := newState.matcherIndex[et]; ok {
			sorted := make([]*Matcher, len(matchers))
			copy(sorted, matchers)
			sortMatchersByPriority(sorted)
			newState.sortedCache[et] = sorted
		}
	}

	e.state.Store(newState)
}

// UpdateCommandCache 更新指定 matcher 的命令缓存（COW 写操作）
func (e *Engine) UpdateCommandCache(m *Matcher) {
	if m == nil {
		return
	}
	cmd := m.GetCommand()
	if cmd == "" {
		return
	}

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	newState := copyEngineState(oldState)
	newState.rebuildCommandInfoCache(m, cmd)
	e.state.Store(newState)
}
