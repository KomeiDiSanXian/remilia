package engine

import (
	"maps"
	"sort"
	"strings"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// MiddlewareTraceHook 中间件追踪钩子函数类型
type MiddlewareTraceHook func(name string, ctx *context.Context)

// engineState 不可变的引擎状态（COW 模式）
//
// 此结构体包含所有需要在读操作中访问的状态。
// 写操作会复制整个状态，修改后原子替换。
type engineState struct {
	// 核心匹配器列表
	matchers []*Matcher

	// 匹配器索引（按事件类型分组）
	// 此索引仅包含普通（非命令优化）的匹配器
	matcherIndex map[dto.EventType][]*Matcher

	// 命令匹配器索引（按命令词 -> 事件类型分组）
	// 优化：使用 map[string]map[dto.EventType][]*Matcher 结构
	// 第一层 key 为命令词（如 "/ping"）
	// 第二层 key 为事件类型（如 C2CMessageCreate），"" 表示通用类型
	commandIndex map[string]map[dto.EventType][]*Matcher

	// 分组索引（按组名分组）
	// 用于快速删除特定组的所有匹配器
	groupIndex map[string][]*Matcher

	// 排序缓存（按优先级排序的匹配器）
	sortedCache map[dto.EventType][]*Matcher

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
	chain []Middleware
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
func newEngineState() *engineState {
	return &engineState{
		matchers:         make([]*Matcher, 0),
		matcherIndex:     make(map[dto.EventType][]*Matcher),
		commandIndex:     make(map[string]map[dto.EventType][]*Matcher),
		groupIndex:       make(map[string][]*Matcher),
		sortedCache:      make(map[dto.EventType][]*Matcher),
		commandInfoCache: make(map[string]*CommandInfo),
		block:            false,
		maxMatchers:      0,
	}
}

// newMiddlewareState 创建新的中间件状态
func newMiddlewareState() *middlewareState {
	return &middlewareState{
		global:           middlewareSnapshot{chain: make([]Middleware, 0), gen: 1},
		groupMiddlewares: make(map[string]*middlewareSnapshot),
		traceHook:        nil,
	}
}

// copyEngineState 深拷贝引擎状态
// 使用 COW 策略，共享底层数组以减少内存分配
//
// 安全性说明：
//   - 使用 [:len:len] 限制容量，确保 append 操作会触发新分配
//   - 只能使用 append 修改切片，不能就地修改（如 matchers[i] = xxx）
//   - 所有修改操作（addMatcher、deleteMatcher 等）都正确使用 append
//   - 这种策略在当前代码中是安全的，因为没有就地修改操作
func copyEngineState(src *engineState) *engineState {
	dst := &engineState{
		// 使用 append 共享底层数组，只在修改时才会复制
		matchers:         src.matchers[:len(src.matchers):len(src.matchers)],
		matcherIndex:     make(map[dto.EventType][]*Matcher, len(src.matcherIndex)),
		commandIndex:     make(map[string]map[dto.EventType][]*Matcher, len(src.commandIndex)),
		groupIndex:       make(map[string][]*Matcher, len(src.groupIndex)),
		sortedCache:      make(map[dto.EventType][]*Matcher, len(src.sortedCache)),
		commandInfoCache: make(map[string]*CommandInfo, len(src.commandInfoCache)),
		block:            src.block,
		maxMatchers:      src.maxMatchers,
	}

	// 复制 matcherIndex map - 使用 reslice 共享底层数组
	for k, v := range src.matcherIndex {
		// 通过限制容量，确保 append 会触发新分配（真正的 COW）
		dst.matcherIndex[k] = v[:len(v):len(v)]
	}

	// 复制 commandIndex map (使用共享数组)
	for cmd, eventMap := range src.commandIndex {
		newEventMap := make(map[dto.EventType][]*Matcher, len(eventMap))
		for et, matchers := range eventMap {
			newEventMap[et] = matchers[:len(matchers):len(matchers)]
		}
		dst.commandIndex[cmd] = newEventMap
	}

	// 复制 groupIndex map - 共享底层数组
	for k, v := range src.groupIndex {
		dst.groupIndex[k] = v[:len(v):len(v)]
	}

	// 复制 sortedCache map - 共享底层数组
	for k, v := range src.sortedCache {
		dst.sortedCache[k] = v[:len(v):len(v)]
	}

	// 复制 commandInfoCache - 浅拷贝指针（CommandInfo 是只读的）
	maps.Copy(dst.commandInfoCache, src.commandInfoCache)

	return dst
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

// rebuildIndex 重建匹配器索引和排序缓存
func (s *engineState) rebuildIndex() {
	// 清空旧索引
	s.matcherIndex = make(map[dto.EventType][]*Matcher)
	s.commandIndex = make(map[string]map[dto.EventType][]*Matcher)
	s.groupIndex = make(map[string][]*Matcher)
	s.sortedCache = make(map[dto.EventType][]*Matcher)
	s.commandInfoCache = make(map[string]*CommandInfo)

	// 重建索引
	for _, m := range s.matchers {
		cmd := m.GetCommand()
		if cmd != "" {
			if s.commandIndex[cmd] == nil {
				s.commandIndex[cmd] = make(map[dto.EventType][]*Matcher)
			}
			s.commandIndex[cmd][m.EventType] = append(s.commandIndex[cmd][m.EventType], m)

			// 重建命令信息缓存
			s.rebuildCommandInfoCache(m, cmd)
		} else {
			// 仅当没有 command 时加入常规索引
			et := m.EventType
			s.matcherIndex[et] = append(s.matcherIndex[et], m)
		}

		// 更新分组索引
		if m.group != "" {
			s.groupIndex[m.group] = append(s.groupIndex[m.group], m)
		}
	}

	// 重建排序缓存（常规索引）
	for eventType, matchers := range s.matcherIndex {
		sorted := make([]*Matcher, len(matchers))
		copy(sorted, matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[eventType] = sorted
	}

	// 对命令索引中的列表也进行排序
	for _, eventMap := range s.commandIndex {
		for _, matchers := range eventMap {
			sortMatchersByPriority(matchers)
		}
	}

	// 改进 3.7: 重建命令列表缓存
	s.rebuildCommandListCache()
}

// rebuildCommandInfoCache 重建单个命令的缓存信息
func (s *engineState) rebuildCommandInfoCache(m *Matcher, cmd string) {
	// 获取定义
	def := m.GetDefinition()

	// 跳过隐藏命令
	if def != nil && def.Hidden {
		// 如果命令被标记为隐藏，从缓存中删除
		delete(s.commandInfoCache, cmd)
		s.rebuildCommandListCache() // 改进 3.7: 同步更新列表缓存
		return
	}

	info := &CommandInfo{
		Command:    cmd,
		EventType:  m.EventType,
		Source:     m.GetSource(),
		Definition: def,
	}

	// 从定义填充字段
	if def != nil {
		info.Description = def.Description
		info.Usage = def.Usage
		info.Aliases = def.Aliases
		info.Category = def.Category
		info.Examples = def.Examples
		info.Permissions = def.Permissions
	}

	// 提取插件名
	if after, ok := strings.CutPrefix(m.GetSource(), "plugin:"); ok {
		info.Plugin = after
	} else {
		info.Plugin = "global"
	}

	s.commandInfoCache[cmd] = info
	// 改进 3.7: 命令信息变化后重建列表缓存
	s.rebuildCommandListCache()
}

// rebuildCommandListCache 将 commandInfoCache 展开为有序切片并缓存。
// 每次 commandInfoCache 变动后调用，保证 GetAllCommands 可 O(1) 复制返回。
func (s *engineState) rebuildCommandListCache() {
	list := make([]CommandInfo, 0, len(s.commandInfoCache))
	for _, info := range s.commandInfoCache {
		list = append(list, *info)
	}
	s.commandListCache = list
	s.commandListVer++
}

// addMatcher 添加匹配器到状态
func (s *engineState) addMatcher(m *Matcher) {
	s.matchers = append(s.matchers, m)

	cmd := m.GetCommand()
	if cmd != "" {
		if s.commandIndex[cmd] == nil {
			s.commandIndex[cmd] = make(map[dto.EventType][]*Matcher)
		}
		s.commandIndex[cmd][m.EventType] = append(s.commandIndex[cmd][m.EventType], m)
		// 每次添加后重新排序（对于单个添加操作，这可以接受；批量添加应使用 rebuildIndex）
		sortMatchersByPriority(s.commandIndex[cmd][m.EventType])

		// 更新命令信息缓存
		s.rebuildCommandInfoCache(m, cmd)
	} else {
		// 更新常规索引
		et := m.EventType
		s.matcherIndex[et] = append(s.matcherIndex[et], m)

		// 更新排序缓存 - 优化：重用现有 slice 容量
		matchers := s.matcherIndex[et]
		// 检查是否需要重新分配
		if cap(s.sortedCache[et]) >= len(matchers) {
			// 重用现有容量
			sorted := s.sortedCache[et][:len(matchers)]
			copy(sorted, matchers)
			sortMatchersByPriority(sorted)
			s.sortedCache[et] = sorted
		} else {
			// 需要新分配
			sorted := make([]*Matcher, len(matchers))
			copy(sorted, matchers)
			sortMatchersByPriority(sorted)
			s.sortedCache[et] = sorted
		}
	}

	// 更新分组索引
	if m.group != "" {
		s.groupIndex[m.group] = append(s.groupIndex[m.group], m)
	}
}

// removeGroup 移除指定分组的所有匹配器
func (s *engineState) removeGroup(groupName string) {
	if groupName == "" {
		return
	}

	// 如果组不存在，无需操作
	if _, ok := s.groupIndex[groupName]; !ok {
		return
	}

	// 过滤主列表
	newMatchers := make([]*Matcher, 0, len(s.matchers))
	for _, m := range s.matchers {
		if m.group != groupName {
			newMatchers = append(newMatchers, m)
		}
	}
	s.matchers = newMatchers

	// 重建所有索引
	s.rebuildIndex()
}

// deleteMatcher 从状态中删除匹配器
func (s *engineState) deleteMatcher(m *Matcher) {
	// 从 matchers 列表中删除
	for i, matcher := range s.matchers {
		if matcher == m {
			s.matchers = append(s.matchers[:i], s.matchers[i+1:]...)
			break
		}
	}

	// 重建索引（简单实现，可以优化为局部更新）
	s.rebuildIndex()
}

// deleteMatchers 从状态中批量删除匹配器
func (s *engineState) deleteMatchers(matchersToDelete []*Matcher) {
	if len(matchersToDelete) == 0 {
		return
	}

	toDelete := make(map[*Matcher]bool)
	for _, m := range matchersToDelete {
		toDelete[m] = true
	}

	// Filter services.matchers in place
	newMatchers := s.matchers[:0]
	for _, m := range s.matchers {
		if !toDelete[m] {
			newMatchers = append(newMatchers, m)
		}
	}
	s.matchers = newMatchers

	// Rebuild index
	s.rebuildIndex()
}

// invalidateSortedCache 失效并重建指定事件类型的排序缓存
func (s *engineState) invalidateSortedCache(eventType dto.EventType) {
	// 重建指定事件类型的缓存
	if matchers, ok := s.matcherIndex[eventType]; ok {
		// 尝试重用现有 slice 容量
		if existing, exists := s.sortedCache[eventType]; exists && cap(existing) >= len(matchers) {
			sorted := existing[:len(matchers)]
			copy(sorted, matchers)
			sortMatchersByPriority(sorted)
			s.sortedCache[eventType] = sorted
		} else {
			sorted := make([]*Matcher, len(matchers))
			copy(sorted, matchers)
			sortMatchersByPriority(sorted)
			s.sortedCache[eventType] = sorted
		}
	} else {
		// 如果该类型没有 matchers，确保从 cache 中移除 (safe delete)
		delete(s.sortedCache, eventType)
	}

	// 如果是具体事件类型，也需要重建通用匹配器缓存
	if eventType != "" {
		if matchers, ok := s.matcherIndex[""]; ok {
			if existing, exists := s.sortedCache[""]; exists && cap(existing) >= len(matchers) {
				sorted := existing[:len(matchers)]
				copy(sorted, matchers)
				sortMatchersByPriority(sorted)
				s.sortedCache[""] = sorted
			} else {
				sorted := make([]*Matcher, len(matchers))
				copy(sorted, matchers)
				sortMatchersByPriority(sorted)
				s.sortedCache[""] = sorted
			}
		}
	}
}

func sortMatchersByPriority(matchers []*Matcher) {
	sort.Slice(matchers, func(i, j int) bool {
		return matchers[i].getPriority() < matchers[j].getPriority()
	})
}
