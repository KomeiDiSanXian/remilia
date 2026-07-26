package engine

import (
	"maps"
	"sort"
	"strings"

	"github.com/KomeiDiSanXian/remilia/core/context"
)

// MiddlewareTraceHook 中间件追踪钩子函数类型
type MiddlewareTraceHook func(name string, ctx *context.Context)

// state 不可变的引擎状态（COW 模式）
//
// 此结构体包含所有需要在读操作中访问的状态。
// 写操作会在新 state 中共享不变部分、只复制需要修改的 map。
type state struct {
	// 核心匹配器列表
	matchers []*Matcher

	// 匹配器索引（按事件类型分组）
	// 此索引仅包含普通（非命令优化）的匹配器
	matcherIndex map[EventType][]*Matcher

	// 命令匹配器索引（按命令词 -> 事件类型分组）
	// 优化：使用 map[string]map[EventType][]*Matcher 结构
	// 第一层 key 为命令词（如 "/ping"）
	// 第二层 key 为事件类型（如 C2CMessageCreate），"" 表示通用类型
	commandIndex map[string]map[EventType][]*Matcher

	// 分组索引（按组名分组）
	// 用于快速删除特定组的所有匹配器
	groupIndex map[string][]*Matcher

	// 排序缓存（按优先级排序的匹配器）
	sortedCache map[EventType][]*Matcher

	// 命令信息缓存（用于优化 GetAllCommands 性能）
	// key 为命令名（如 "/ping"），value 为命令信息
	commandInfoCache map[string]*CommandInfo

	// 改进 3.7: commandListCache 是 commandInfoCache 展开后的只读切片缓存，
	// 避免 GetAllCommands 每次调用都重新分配切片并遍历 map。
	// commandListVer 与 commandInfoCache 同步更新，用于缓存失效检测。
	commandListCache []CommandInfo
	commandListVer   int64

	// 配置项
	block       bool // 是否阻断后续匹配器
	maxMatchers int  // 最大匹配器数量限制
}

// middlewareSnapshot 表示不可变的中间件切片及其代际号
// gen 每次修改对应切片时递增，用于缓存失效
type middlewareSnapshot struct {
	chain []context.Middleware
	gen   uint64
}

// middlewareState 不可变的中间件状态（COW 模式）
//
// 中间件配置独立于引擎状态，可以单独更新。
type middlewareState struct {
	// 全局中间件快照
	global middlewareSnapshot

	// 分组中间件快照（按组名分组）
	groupMiddlewares map[string]*middlewareSnapshot

	// traceHook is called when a named middleware executes (可以为 nil).
	traceHook *MiddlewareTraceHook
}

// newEngineState 创建新的引擎状态
func newEngineState() *state {
	return &state{
		matchers:         make([]*Matcher, 0),
		matcherIndex:     make(map[EventType][]*Matcher),
		commandIndex:     make(map[string]map[EventType][]*Matcher),
		groupIndex:       make(map[string][]*Matcher),
		sortedCache:      make(map[EventType][]*Matcher),
		commandInfoCache: make(map[string]*CommandInfo),
		block:            false,
		maxMatchers:      0,
	}
}

// newMiddlewareState 创建新的中间件状态
func newMiddlewareState() *middlewareState {
	return &middlewareState{
		global:           middlewareSnapshot{chain: make([]context.Middleware, 0), gen: 1},
		groupMiddlewares: make(map[string]*middlewareSnapshot),
		traceHook:        nil,
	}
}

// ============================================================================
// Map 复制辅助函数（COW 语义）
// ============================================================================

// copyMatcherIndex 复制 matcherIndex，子切片共享底层数组（cap=len 确保 COW）
func copyMatcherIndex(src map[EventType][]*Matcher) map[EventType][]*Matcher {
	if src == nil {
		return nil
	}
	dst := make(map[EventType][]*Matcher, len(src))
	for k, v := range src {
		dst[k] = v[:len(v):len(v)]
	}
	return dst
}

// copyCommandIndex 复制 commandIndex，内层 map 和子切片都 COW
func copyCommandIndex(src map[string]map[EventType][]*Matcher) map[string]map[EventType][]*Matcher {
	if src == nil {
		return nil
	}
	dst := make(map[string]map[EventType][]*Matcher, len(src))
	for cmd, eventMap := range src {
		newEventMap := make(map[EventType][]*Matcher, len(eventMap))
		for et, matchers := range eventMap {
			newEventMap[et] = matchers[:len(matchers):len(matchers)]
		}
		dst[cmd] = newEventMap
	}
	return dst
}

// copyGroupIndex 复制 groupIndex，子切片共享底层数组
func copyGroupIndex(src map[string][]*Matcher) map[string][]*Matcher {
	if src == nil {
		return nil
	}
	dst := make(map[string][]*Matcher, len(src))
	for k, v := range src {
		dst[k] = v[:len(v):len(v)]
	}
	return dst
}

// ============================================================================
// with* 方法 — 选择性 COW，只复制需要修改的 map
// ============================================================================

// clone 完整深拷贝所有 map（仅在测试中使用，生产代码使用 with* 方法做选择性 COW）
func (s *state) clone() *state {
	dst := &state{
		matchers:         s.matchers[:len(s.matchers):len(s.matchers)],
		matcherIndex:     copyMatcherIndex(s.matcherIndex),
		commandIndex:     copyCommandIndex(s.commandIndex),
		groupIndex:       copyGroupIndex(s.groupIndex),
		sortedCache:      copySortedCacheForInvalidation(s.sortedCache),
		commandInfoCache: make(map[string]*CommandInfo, len(s.commandInfoCache)),
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            s.block,
		maxMatchers:      s.maxMatchers,
	}
	maps.Copy(dst.commandInfoCache, s.commandInfoCache)
	return dst
}

// withBlock 仅修改 block 字段，共享所有 map
func (s *state) withBlock(block bool) *state {
	return &state{
		matchers:         s.matchers,
		matcherIndex:     s.matcherIndex,
		commandIndex:     s.commandIndex,
		groupIndex:       s.groupIndex,
		sortedCache:      s.sortedCache,
		commandInfoCache: s.commandInfoCache,
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            block,
		maxMatchers:      s.maxMatchers,
	}
}

// withMaxMatchers 仅修改 maxMatchers 字段，共享所有 map
func (s *state) withMaxMatchers(max int) *state {
	return &state{
		matchers:         s.matchers,
		matcherIndex:     s.matcherIndex,
		commandIndex:     s.commandIndex,
		groupIndex:       s.groupIndex,
		sortedCache:      s.sortedCache,
		commandInfoCache: s.commandInfoCache,
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            s.block,
		maxMatchers:      max,
	}
}

// withClearedMatchers 清空所有匹配器和索引
func (s *state) withClearedMatchers() *state {
	return &state{
		matchers:         make([]*Matcher, 0),
		matcherIndex:     make(map[EventType][]*Matcher),
		commandIndex:     make(map[string]map[EventType][]*Matcher),
		groupIndex:       make(map[string][]*Matcher),
		sortedCache:      make(map[EventType][]*Matcher),
		commandInfoCache: make(map[string]*CommandInfo),
		commandListCache: nil,
		block:            s.block,
		maxMatchers:      s.maxMatchers,
	}
}

// withAddedMatcher 添加一个匹配器，仅复制需要的 map
//
// 索引分类以 m.commandIndexed 标志为准（由 OnCommand/RegisterCommandDef 在注册前置位）：
// 仅设置了 Definition.Name 的普通 matcher 保留在常规索引中按规则匹配，
// 避免其被移入 commandIndex 后 Match 跳过 Rules[0] 造成语义漂移。
func (s *state) withAddedMatcher(m *Matcher) *state {
	cmd := m.GetCommand()
	et := m.EventType
	grp := m.GetGroup()

	dst := &state{
		matchers:         append(s.matchers[:len(s.matchers):len(s.matchers)], m),
		commandInfoCache: s.commandInfoCache,
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            s.block,
		maxMatchers:      s.maxMatchers,
	}

	if cmd != "" && m.commandIndexed.Load() {
		dst.sortedCache = s.sortedCache
		dst.matcherIndex = s.matcherIndex
		dst.commandIndex = s.copyCommandIndexWithEntry(cmd, et, m)
		dst.rebuildCommandInfoCache(m, cmd)
	} else {
		dst.sortedCache = copySortedCacheForInvalidation(s.sortedCache)
		dst.matcherIndex = copyMatcherIndex(s.matcherIndex)
		dst.matcherIndex[et] = append(dst.matcherIndex[et][:len(dst.matcherIndex[et]):len(dst.matcherIndex[et])], m)
		dst.commandIndex = s.commandIndex

		matchers := dst.matcherIndex[et]
		sorted := makeRunnableSlice(matchers)
		sortMatchersByPriority(sorted)
		dst.sortedCache[et] = sorted

		// 常规索引中的命令名 matcher（如 BindCommand/SetDefinition 补充了 Name）
		// 同样维护 help 元数据缓存
		if cmd != "" {
			dst.rebuildCommandInfoCache(m, cmd)
		}
	}

	if grp != "" {
		dst.groupIndex = copyGroupIndex(s.groupIndex)
		dst.groupIndex[grp] = append(dst.groupIndex[grp][:len(dst.groupIndex[grp]):len(dst.groupIndex[grp])], m)
	} else {
		dst.groupIndex = s.groupIndex
	}

	return dst
}

// copyCommandIndexWithEntry 复制 commandIndex 并添加一个新条目
func (s *state) copyCommandIndexWithEntry(cmd string, et EventType, m *Matcher) map[string]map[EventType][]*Matcher {
	dst := make(map[string]map[EventType][]*Matcher, len(s.commandIndex))
	for c, eventMap := range s.commandIndex {
		newEventMap := make(map[EventType][]*Matcher, len(eventMap))
		for e, matchers := range eventMap {
			newEventMap[e] = matchers[:len(matchers):len(matchers)]
		}
		dst[c] = newEventMap
	}
	if _, ok := dst[cmd]; !ok {
		dst[cmd] = make(map[EventType][]*Matcher)
	}
	dst[cmd][et] = append(dst[cmd][et][:len(dst[cmd][et]):len(dst[cmd][et])], m)
	sortMatchersByPriority(dst[cmd][et])
	return dst
}

// withBatchMatchers 批量添加多个匹配器，仅复制一次 maps
//
// COW 安全性说明：copy*Index 复制出的子切片与旧 state 共享底层数组（cap=len），
// 只有本批次 append 过的键才拥有独立数组、可以安全就地排序。
// 绝不能对未触及的键就地排序——那会改写已发布旧 state 中
// 正被 processEventMatchers 无锁读取的数组（数据竞争）。
func (s *state) withBatchMatchers(matchers []*Matcher) *state {
	if len(matchers) == 0 {
		return s
	}

	dst := &state{
		matchers:         append(s.matchers[:len(s.matchers):len(s.matchers)], matchers...),
		matcherIndex:     copyMatcherIndex(s.matcherIndex),
		commandIndex:     copyCommandIndex(s.commandIndex),
		groupIndex:       copyGroupIndex(s.groupIndex),
		sortedCache:      copySortedCacheForInvalidation(s.sortedCache),
		commandInfoCache: maps.Clone(s.commandInfoCache),
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            s.block,
		maxMatchers:      s.maxMatchers,
	}

	// 记录本批次实际修改过的键，仅对这些键重排/重建缓存
	touchedEvents := make(map[EventType]struct{}, len(matchers))
	type cmdKey struct {
		cmd string
		et  EventType
	}
	touchedCmds := make(map[cmdKey]struct{}, len(matchers))
	infoDirty := false

	for _, m := range matchers {
		// 与 addMatcher 一致：直接设置 Handler 字段的 matcher 也纳入可运行集合
		if m.Handler != nil {
			m.hasHandler.Store(true)
		}

		cmd := m.GetCommand()
		et := m.EventType
		grp := m.GetGroup()

		// 维护 help 元数据（无论进入哪个索引）
		if cmd != "" {
			dst.updateCommandInfoCache(m, cmd)
			infoDirty = true
		}

		// 索引分类以 commandIndexed 标志为准（见 withAddedMatcher 注释）
		if cmd != "" && m.commandIndexed.Load() {
			if dst.commandIndex[cmd] == nil {
				dst.commandIndex[cmd] = make(map[EventType][]*Matcher)
			}
			dst.commandIndex[cmd][et] = append(dst.commandIndex[cmd][et], m)
			touchedCmds[cmdKey{cmd: cmd, et: et}] = struct{}{}
		} else {
			dst.matcherIndex[et] = append(dst.matcherIndex[et], m)
			touchedEvents[et] = struct{}{}
		}

		if grp != "" {
			dst.groupIndex[grp] = append(dst.groupIndex[grp], m)
		}
	}

	// 仅重建受影响事件类型的排序缓存（makeRunnableSlice 分配新切片，排序安全）
	for et := range touchedEvents {
		sorted := makeRunnableSlice(dst.matcherIndex[et])
		sortMatchersByPriority(sorted)
		dst.sortedCache[et] = sorted
	}

	// 仅排序本批次 append 过的命令桶：append 已使其底层数组独立于旧 state
	for k := range touchedCmds {
		sortMatchersByPriority(dst.commandIndex[k.cmd][k.et])
	}

	if infoDirty {
		dst.rebuildCommandListCache()
	}
	return dst
}

// withDeletedMatcher 删除一个匹配器（内部通过 rebuildIndex 重建所有索引）
func (s *state) withDeletedMatcher(m *Matcher) *state {
	idx := -1
	for i, matcher := range s.matchers {
		if matcher == m {
			idx = i
			break
		}
	}
	if idx < 0 {
		return s
	}

	dst := &state{
		matchers:    make([]*Matcher, 0, len(s.matchers)-1),
		block:       s.block,
		maxMatchers: s.maxMatchers,
	}
	dst.matchers = append(dst.matchers, s.matchers[:idx]...)
	dst.matchers = append(dst.matchers, s.matchers[idx+1:]...)
	dst.rebuildIndex()
	return dst
}

// withDeletedMatchers 批量删除匹配器（内部通过 rebuildIndex 重建所有索引）
func (s *state) withDeletedMatchers(matchersToDelete []*Matcher) *state {
	if len(matchersToDelete) == 0 {
		return s
	}

	toDelete := make(map[*Matcher]struct{}, len(matchersToDelete))
	for _, m := range matchersToDelete {
		toDelete[m] = struct{}{}
	}

	estimated := max(len(s.matchers)-len(matchersToDelete), 0)
	dst := &state{
		matchers:    make([]*Matcher, 0, estimated),
		block:       s.block,
		maxMatchers: s.maxMatchers,
	}
	for _, m := range s.matchers {
		if _, ok := toDelete[m]; !ok {
			dst.matchers = append(dst.matchers, m)
		}
	}
	dst.rebuildIndex()
	return dst
}

// withRemovedGroup 删除指定分组的所有匹配器
func (s *state) withRemovedGroup(groupName string) *state {
	if groupName == "" {
		return s
	}

	dst := &state{
		matchers:    make([]*Matcher, 0, len(s.matchers)),
		block:       s.block,
		maxMatchers: s.maxMatchers,
	}
	for _, m := range s.matchers {
		if m.GetGroup() != groupName {
			dst.matchers = append(dst.matchers, m)
		}
	}

	if len(dst.matchers) == len(s.matchers) {
		return s
	}

	dst.rebuildIndex()
	return dst
}

// withRebuiltIndex 从 matchers 重建所有索引
func (s *state) withRebuiltIndex() *state {
	dst := &state{
		matchers:    s.matchers,
		block:       s.block,
		maxMatchers: s.maxMatchers,
	}
	dst.rebuildIndex()
	return dst
}

// withInvalidatedSortedCache 仅重建 sortedCache 中指定事件类型的条目
func (s *state) withInvalidatedSortedCache(eventType EventType) *state {
	dst := &state{
		matchers:         s.matchers,
		matcherIndex:     s.matcherIndex,
		commandIndex:     s.commandIndex,
		groupIndex:       s.groupIndex,
		commandInfoCache: s.commandInfoCache,
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            s.block,
		maxMatchers:      s.maxMatchers,
		sortedCache:      copySortedCacheForInvalidation(s.sortedCache),
	}

	dst.invalidateSortedCache(eventType)

	if eventType != "" {
		if _, exists := dst.sortedCache[""]; !exists {
			if mats, ok := dst.matcherIndex[""]; ok {
				sorted := makeRunnableSlice(mats)
				sortMatchersByPriority(sorted)
				dst.sortedCache[""] = sorted
			}
		}
	}

	return dst
}

// copySortedCacheForInvalidation 复制 sortedCache（共享子切片）
func copySortedCacheForInvalidation(src map[EventType][]*Matcher) map[EventType][]*Matcher {
	dst := make(map[EventType][]*Matcher, len(src))
	for k, v := range src {
		dst[k] = v[:len(v):len(v)]
	}
	return dst
}

// withUpdatedMatcherIndex 仅更新 affected matcher 的索引
func (s *state) withUpdatedMatcherIndex(m *Matcher) *state {
	if m == nil {
		return s.withRebuiltIndex()
	}

	cmd := m.GetCommand()
	et := m.EventType

	dst := &state{
		matchers:         s.matchers,
		matcherIndex:     s.matcherIndex,
		commandIndex:     s.commandIndex,
		groupIndex:       s.groupIndex,
		sortedCache:      copySortedCacheForInvalidation(s.sortedCache),
		commandInfoCache: s.commandInfoCache,
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            s.block,
		maxMatchers:      s.maxMatchers,
	}

	if cmd != "" && m.commandIndexed.Load() {
		// 重排命令桶时必须先做逐元素完整拷贝：
		// copyCommandIndex 的子切片与旧 state 共享底层数组（COW-append 语义），
		// 直接就地排序会改写已发布旧 state 中正被无锁读取的数组（数据竞争）。
		if em, ok := s.commandIndex[cmd]; ok {
			if lst, ok := em[et]; ok {
				dst.commandIndex = copyCommandIndex(s.commandIndex)
				sorted := append([]*Matcher(nil), lst...)
				sortMatchersByPriority(sorted)
				dst.commandIndex[cmd][et] = sorted
			}
		}
	} else {
		if matchers, ok := dst.matcherIndex[et]; ok {
			sorted := makeRunnableSlice(matchers)
			sortMatchersByPriority(sorted)
			dst.sortedCache[et] = sorted
		}
	}

	return dst
}

// withUpdatedCommandCache 仅更新指定 matcher 的命令缓存
func (s *state) withUpdatedCommandCache(m *Matcher) *state {
	cmd := m.GetCommand()
	if cmd == "" {
		return s
	}

	dst := &state{
		matchers:         s.matchers,
		matcherIndex:     s.matcherIndex,
		commandIndex:     s.commandIndex,
		groupIndex:       s.groupIndex,
		sortedCache:      s.sortedCache,
		commandInfoCache: maps.Clone(s.commandInfoCache),
		block:            s.block,
		maxMatchers:      s.maxMatchers,
	}
	dst.rebuildCommandInfoCache(m, cmd)
	return dst
}

// withSetMatcherGroup 更新 groupIndex（单个 matcher 分组变更）
func (s *state) withSetMatcherGroup(m *Matcher, oldGroup, newGroup string) *state {
	dst := &state{
		matchers:         s.matchers,
		matcherIndex:     s.matcherIndex,
		commandIndex:     s.commandIndex,
		sortedCache:      s.sortedCache,
		commandInfoCache: s.commandInfoCache,
		commandListCache: s.commandListCache,
		commandListVer:   s.commandListVer,
		block:            s.block,
		maxMatchers:      s.maxMatchers,
		groupIndex:       copyGroupIndex(s.groupIndex),
	}

	if oldGroup != "" {
		filtered := make([]*Matcher, 0, len(dst.groupIndex[oldGroup]))
		for _, gm := range dst.groupIndex[oldGroup] {
			if gm != m {
				filtered = append(filtered, gm)
			}
		}
		if len(filtered) == 0 {
			delete(dst.groupIndex, oldGroup)
		} else {
			dst.groupIndex[oldGroup] = filtered
		}
	}

	if newGroup != "" {
		dst.groupIndex[newGroup] = append(dst.groupIndex[newGroup], m)
	}

	return dst
}

// ============================================================================
// 原变更方法（仅供测试和新 state 内部使用）
// ============================================================================

// rebuildIndex 重建匹配器索引和排序缓存
func (s *state) rebuildIndex() {
	// 清空旧索引
	s.matcherIndex = make(map[EventType][]*Matcher)
	s.commandIndex = make(map[string]map[EventType][]*Matcher)
	s.groupIndex = make(map[string][]*Matcher)
	s.sortedCache = make(map[EventType][]*Matcher)
	s.commandInfoCache = make(map[string]*CommandInfo)
	s.commandListCache = nil
	s.commandListVer = 0

	// 重建索引
	for _, m := range s.matchers {
		cmd := m.GetCommand()

		// 命令元数据缓存与索引分类解耦：只要有命令名就维护 help 元数据。
		// 仅更新 commandInfoCache，不触发列表缓存重建（O(N²) 修复）
		if cmd != "" {
			s.updateCommandInfoCache(m, cmd)
		}

		// 仅 OnCommand/RegisterCommandDef 创建（commandIndexed 已在注册前置位）的
		// matcher 进入 O(1) 命令索引；普通 matcher 即使事后通过 SetDefinition/
		// BindCommand 补充了 Definition.Name，也保留在常规索引中按全部规则匹配，
		// 避免重建后 Match 跳过 Rules[0]（它未必是 OnCommand 规则）造成语义漂移。
		if cmd != "" && m.commandIndexed.Load() {
			if s.commandIndex[cmd] == nil {
				s.commandIndex[cmd] = make(map[EventType][]*Matcher)
			}
			s.commandIndex[cmd][m.EventType] = append(s.commandIndex[cmd][m.EventType], m)
		} else {
			et := m.EventType
			s.matcherIndex[et] = append(s.matcherIndex[et], m)
		}

		// 更新分组索引（使用加锁访问器避免与 SetMatcherGroup 的数据竞态）
		if grp := m.GetGroup(); grp != "" {
			s.groupIndex[grp] = append(s.groupIndex[grp], m)
		}
	}

	// 重建排序缓存（常规索引）
	for eventType, matchers := range s.matcherIndex {
		sorted := makeRunnableSlice(matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[eventType] = sorted
	}

	// 对命令索引中的列表也进行排序
	for _, eventMap := range s.commandIndex {
		for _, matchers := range eventMap {
			sortMatchersByPriority(matchers)
		}
	}

	// 改进 3.7: 重建命令列表缓存（只调用一次，修复 O(N²) 问题）
	s.rebuildCommandListCache()
}

// buildCommandInfo 从 Matcher 构造 CommandInfo（消除 updateCommandInfoCache 与 rebuildCommandInfoCache 的重复）
func buildCommandInfo(m *Matcher, cmd string) *CommandInfo {
	def := m.GetDefinition()
	info := &CommandInfo{
		Command:    cmd,
		EventType:  m.EventType,
		Source:     m.GetSource(),
		Definition: def,
	}
	if def != nil {
		info.Description = def.Description
		info.Usage = def.Usage
		info.Aliases = def.Aliases
		info.Category = def.Category
		info.Examples = def.Examples
		info.Permissions = def.Permissions
	}
	if after, ok := strings.CutPrefix(m.GetSource(), "plugin:"); ok {
		info.Plugin = after
	} else {
		info.Plugin = "global"
	}
	return info
}

// updateCommandInfoCache 更新单个命令的缓存信息（不重建列表缓存）。
// 供 rebuildIndex 批量调用，避免 O(N²) 问题。
// 调用者负责在全部更新完成后统一调用 rebuildCommandListCache。
func (s *state) updateCommandInfoCache(m *Matcher, cmd string) {
	def := m.GetDefinition()
	if def != nil && def.Hidden {
		delete(s.commandInfoCache, cmd)
		return
	}
	s.commandInfoCache[cmd] = buildCommandInfo(m, cmd)
}

// rebuildCommandInfoCache 重建单个命令的缓存信息并同步更新列表缓存。
// 供单次更新路径（addMatcher、UpdateCommandCache）使用。
func (s *state) rebuildCommandInfoCache(m *Matcher, cmd string) {
	def := m.GetDefinition()
	if def != nil && def.Hidden {
		delete(s.commandInfoCache, cmd)
		s.rebuildCommandListCache()
		return
	}
	s.commandInfoCache[cmd] = buildCommandInfo(m, cmd)
	s.rebuildCommandListCache()
}

// rebuildCommandListCache 将 commandInfoCache 展开为有序切片并缓存。
// 每次 commandInfoCache 变动后调用，保证 GetAllCommands 可 O(1) 复制返回。
func (s *state) rebuildCommandListCache() {
	list := make([]CommandInfo, 0, len(s.commandInfoCache))
	for _, info := range s.commandInfoCache {
		list = append(list, *info)
	}
	s.commandListCache = list
	s.commandListVer++
}

// addMatcher 添加匹配器到状态（仅供测试用，生产代码使用 withAddedMatcher）
func (s *state) addMatcher(m *Matcher) {
	s.matchers = append(s.matchers, m)

	// 自动推导 hasHandler：直接设置 Handler 字段（非 Handle() 调用）也生效
	if m.Handler != nil {
		m.hasHandler.Store(true)
	}

	cmd := m.GetCommand()
	if cmd != "" {
		m.commandIndexed.Store(true)
		if s.commandIndex[cmd] == nil {
			s.commandIndex[cmd] = make(map[EventType][]*Matcher)
		}
		s.commandIndex[cmd][m.EventType] = append(s.commandIndex[cmd][m.EventType], m)
		sortMatchersByPriority(s.commandIndex[cmd][m.EventType])
		s.rebuildCommandInfoCache(m, cmd)
	} else {
		et := m.EventType
		s.matcherIndex[et] = append(s.matcherIndex[et], m)

		matchers := s.matcherIndex[et]
		sorted := makeRunnableSlice(matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[et] = sorted
	}

	if grp := m.GetGroup(); grp != "" {
		s.groupIndex[grp] = append(s.groupIndex[grp], m)
	}
}

// removeGroup 移除指定分组的所有匹配器（仅供测试用，生产代码使用 withRemovedGroup）
func (s *state) removeGroup(groupName string) {
	if groupName == "" {
		return
	}
	if _, ok := s.groupIndex[groupName]; !ok {
		return
	}

	newMatchers := make([]*Matcher, 0, len(s.matchers))
	for _, m := range s.matchers {
		if m.GetGroup() != groupName {
			newMatchers = append(newMatchers, m)
		}
	}
	s.matchers = newMatchers
	s.rebuildIndex()
}

// deleteMatcher 从状态中删除匹配器（仅供测试用，生产代码使用 withDeletedMatcher）
func (s *state) deleteMatcher(m *Matcher) {
	s.matchers = deleteFromSlice(s.matchers, m)
	s.rebuildIndex()
}

// deleteMatchers 从状态中批量删除匹配器（仅供测试用，生产代码使用 withDeletedMatchers）
func (s *state) deleteMatchers(matchersToDelete []*Matcher) {
	if len(matchersToDelete) == 0 {
		return
	}

	toDelete := make(map[*Matcher]struct{}, len(matchersToDelete))
	for _, m := range matchersToDelete {
		toDelete[m] = struct{}{}
	}

	newMatchers := make([]*Matcher, 0, len(s.matchers))
	for _, m := range s.matchers {
		if _, ok := toDelete[m]; !ok {
			newMatchers = append(newMatchers, m)
		}
	}
	s.matchers = newMatchers
	s.rebuildIndex()
}

// deleteFromSlice 删除切片中第一个匹配的元素（辅助函数）
func deleteFromSlice[T comparable](slice []T, elem T) []T {
	for i, v := range slice {
		if v == elem {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// makeRunnableSlice 从 matchers 中过滤出 hasHandler == true 的匹配器
func makeRunnableSlice(matchers []*Matcher) []*Matcher {
	sorted := make([]*Matcher, 0, len(matchers))
	for _, m := range matchers {
		if m.hasHandler.Load() {
			sorted = append(sorted, m)
		}
	}
	return sorted
}

// invalidateSortedCache 失效并重建指定事件类型的排序缓存
func (s *state) invalidateSortedCache(eventType EventType) {
	if matchers, ok := s.matcherIndex[eventType]; ok {
		sorted := makeRunnableSlice(matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[eventType] = sorted
	} else {
		delete(s.sortedCache, eventType)
	}

	if eventType != "" {
		if matchers, ok := s.matcherIndex[""]; ok {
			sorted := makeRunnableSlice(matchers)
			sortMatchersByPriority(sorted)
			s.sortedCache[""] = sorted
		}
	}
}

func sortMatchersByPriority(matchers []*Matcher) {
	sort.Slice(matchers, func(i, j int) bool {
		return matchers[i].getPriority() < matchers[j].getPriority()
	})
}

// copyMiddlewareState 深拷贝中间件状态
// 使用 COW 策略，共享底层数组以减少内存分配
func copyMiddlewareState(src *middlewareState) *middlewareState {
	dst := &middlewareState{
		global: middlewareSnapshot{
			// 共享底层数组，限制容量实现 COW
			chain: src.global.chain[:len(src.global.chain):len(src.global.chain)],
			gen:   src.global.gen,
		},
		groupMiddlewares: make(map[string]*middlewareSnapshot, len(src.groupMiddlewares)),
		traceHook:        src.traceHook,
	}

	// 复制分组中间件 - 使用共享数组
	for k, v := range src.groupMiddlewares {
		dst.groupMiddlewares[k] = &middlewareSnapshot{
			chain: v.chain[:len(v.chain):len(v.chain)],
			gen:   v.gen,
		}
	}

	return dst
}
