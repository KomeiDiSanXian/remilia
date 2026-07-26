package engine

// engine_matcher_ops.go — Matcher 注册、删除、分组、迁移等写操作
//
// 本文件包含所有与 Matcher 生命周期相关的写操作：
//   - 注册（On/OnEventKind/OnTemp/BatchRegisterMatchers）
//   - 删除（DeleteMatcher/DeleteMatchers/DeleteAllMatchers）
//   - 分组（SetMatcherGroup/WithMatcherGroupBatch/RemoveGroup）
//   - 状态修改（SetBlock/SetMaxMatchers/InvalidateSortedCache）
//   - 迁移（MigrateMatcherToTemp/FromTemp）
//   - 索引维护（UpdateMatcherIndex/UpdateMatcherCommand/UpdateCommandCache）

import (
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

func newNoopMatcher(e *Engine) *Matcher {
	m := &Matcher{
		Source:      "noop",
		Rules:       []context.Rule{},
		middlewares: []context.Middleware{},
		coordinator: e,
		execProfile: newExecProfile(),
	}
	m.priority.Store(999)
	m.rt.deleted.Store(true)
	return m
}

// ---- 删除操作 ----------------------------------------------------------------

// DeleteAllMatchers 删除引擎中的所有匹配器（COW 写操作）
func (e *Engine) DeleteAllMatchers() {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldState := e.state.Load()
	oldMatchers := append([]*Matcher(nil), oldState.matchers...)

	e.state.Store(oldState.withClearedMatchers())

	for _, m := range oldMatchers {
		if m == nil {
			continue
		}
		m.rt.deleted.Store(true)
	}
}

// DeleteMatcher 删除指定的匹配器。
//
// 语义：
//   - matcher 会**立即**被标记为 deleted（Match 即刻拒绝，不再命中新事件）；
//   - 从索引/状态中的物理移除交给批量删除处理器**异步**完成
//     （默认每 100ms 一批，见 WithPendingDeleteProcessInterval），
//     单次 COW 全量重建即可回收整批 matcher，避免高频删除时的写放大；
//   - 因此 GetMatcherCount/GetMatcherStats 在一个处理间隔内可能仍计入该 matcher，
//     需要确定性时机时可调用 [Engine.FlushPendingDeletes]。
//
// 回退：处理器未运行（WithNoBackgroundWorkers / 间隔为 0 / 已 Shutdown）
// 或队列已满时，退化为同步 COW 删除，删除永不丢失。
func (e *Engine) DeleteMatcher(m *Matcher) {
	if m == nil {
		return
	}
	// 先置 deleted：无论走哪条路径，matcher 都立即停止匹配
	m.rt.deleted.Store(true)

	if e.internals.pendingDeleteActive.Load() {
		select {
		case e.internals.pendingDeleteCh <- m:
			return
		default:
			// 队列满：退化为同步删除
		}
	}

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	state := e.state.Load()
	e.state.Store(state.withDeletedMatcher(m))
}

// FlushPendingDeletes 立即处理批量删除队列中所有待删除的 matcher。
//
// 供测试和需要确定性删除时机的调用方使用；Shutdown 收尾时也会调用一次。
// 并发安全：内部通过 writeMu 串行化实际删除。
func (e *Engine) FlushPendingDeletes() {
	for {
		e.processPendingDeletes()
		if len(e.internals.pendingDeleteCh) == 0 {
			return
		}
	}
}

// DeleteMatchers 批量删除匹配器（COW 写操作）
func (e *Engine) DeleteMatchers(matchers []*Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	state := e.state.Load()
	e.state.Store(state.withDeletedMatchers(matchers))
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

	e.state.Store(oldState.withAddedMatcher(m))

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

	e.state.Store(oldState.withBatchMatchers(matchers))

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
		Source:      "global",
		execProfile: newExecProfile(),
	}
	matcher.priority.Store(50)
	return e.registerMatcher(matcher)
}

// OnAny 注册一个适用于所有事件类型的匹配器
func (e *Engine) OnAny(rules ...context.Rule) *Matcher {
	return e.On("", rules...)
}

// OnEventKind 注册处理指定平台事件类别的匹配器（平台无关，推荐使用）
//
// 示例：
//
//	engine.OnEventKind(platform.EventKindPrivateMessage, context.OnCommand("/ping")).Handle(handler)
//	engine.OnEventKind(platform.EventKindGroupMessage, context.OnCommand("/help")).Handle(handler)
func (e *Engine) OnEventKind(kind platform.EventKind, rules ...context.Rule) *Matcher {
	return e.On(string(kind), rules...)
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
		Source:      "temp",
		execProfile: newExecProfile(),
		rt: matcherRuntime{
			isTemp:      1,
			maxUseCount: 1,
		},
	}
	matcher.priority.Store(50)
	e.internals.tempManager.Add(matcher)
	e.rebuildMatcherChainCOW(matcher)
	return matcher
}

// ---- 状态修改操作 -------------------------------------------------------------

// InvalidateSortedCache 失效指定事件类型的排序缓存（COW 写操作）
func (e *Engine) InvalidateSortedCache(eventType EventType) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	state := e.state.Load()
	e.state.Store(state.withInvalidatedSortedCache(eventType))
}

// SetBlock 设置引擎的阻塞状态（COW 写操作）
func (e *Engine) SetBlock(block bool) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	e.state.Store(e.state.Load().withBlock(block))
	return e
}

// SetMaxMatchers 设置匹配器数量上限（COW 写操作）
// 设置为 0 表示不限制（默认）
func (e *Engine) SetMaxMatchers(limit int) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	e.state.Store(e.state.Load().withMaxMatchers(limit))
	return e
}

// EnableGlobalMatchers 启用/禁用全局匹配器。
//
// 语义说明：本方法等价于 SetBlock(!enable)。SetBlock(true) 的含义是
// "首个命中的 matcher 执行后阻断其余 matcher"——因此 EnableGlobalMatchers(false)
// **并不会**让所有 matcher 停止响应：每个事件仍会执行优先级最高的一个命中者。
// 若需要完全停止事件分发，请使用 DisableGroup 逐组禁用，或在上游停止投递事件。
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

	e.state.Store(state.withRemovedGroup(groupName))

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

	e.state.Store(e.state.Load().withRebuiltIndex())
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
	sourceChanged := source != "" && m.Source != source
	if source != "" {
		m.Source = source
	}
	m.invalidateCombinedChain()
	m.rt.mu.Unlock()

	if oldGroup != m.group {
		e.writeMu.Lock()
		newGroupName := strings.TrimSpace(group)
		e.state.Store(e.state.Load().withSetMatcherGroup(m, oldGroup, newGroupName))
		e.writeMu.Unlock()
	}

	// Source 变更会影响 CommandInfo 的 Source/Plugin 字段；
	// 同步更新 commandInfoCache，确保 GetAllCommands/FindCommand 反映新来源。
	// 与 SetSource 的行为保持一致（SetSource 也调用 UpdateCommandCache）。
	if sourceChanged {
		e.UpdateCommandCache(m)
	}

	e.rebuildMatcherChainCOW(m)
}

// ---- 迁移操作 ----------------------------------------------------------------

// UpdateTempMatcherPriority 更新临时 matcher 的优先级（内部方法）
func (e *Engine) UpdateTempMatcherPriority(m *Matcher) {
	e.internals.tempManager.Remove(m)
	e.internals.tempManager.Add(m)
}

// setTempMatcherExpiration 实现 tempExpirationSetter：
// 在 TempManager 的 shard 锁内写入 createdAt/expiresAt，并在 matcher
// 已由管理器持有时补登过期堆（见 Matcher.SetTempWithTimeout）。
func (e *Engine) setTempMatcherExpiration(m *Matcher, createdAt, expiresAt time.Time) {
	e.internals.tempManager.SetExpiration(m, createdAt, expiresAt)
}

// MigrateMatcherToTemp 将 matcher 迁移到 TempManager
//
// 先移除状态再添加到 TempManager，避免并发 ProcessEvent 过程中
// matcher 同时在两个位置出现导致双倍执行。
// 迁移窗口内该 matcher 可能短暂丢失事件——属于可接受行为。
func (e *Engine) MigrateMatcherToTemp(m *Matcher) {
	e.removeMatcherFromStateSilently(m)
	e.internals.tempManager.Add(m)
}

// MigrateMatcherFromTemp 将 matcher 从 TempManager 迁移到 State
//
// 先从 TempManager 移除再添加到状态，防止并发 ProcessEvent 双倍执行。
func (e *Engine) MigrateMatcherFromTemp(m *Matcher) {
	e.internals.tempManager.Remove(m)
	e.addMatcherToStateSilently(m)
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

	e.state.Store(e.state.Load().withUpdatedMatcherIndex(m))
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

	e.state.Store(e.state.Load().withUpdatedCommandCache(m))
}
