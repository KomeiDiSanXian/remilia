package engine

import (
	"sort"

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
		matchers:     make([]*Matcher, 0),
		matcherIndex: make(map[dto.EventType][]*Matcher),
		commandIndex: make(map[string]map[dto.EventType][]*Matcher),
		groupIndex:   make(map[string][]*Matcher),
		sortedCache:  make(map[dto.EventType][]*Matcher),
		block:        false,
		maxMatchers:  0,
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
func copyEngineState(src *engineState) *engineState {
	dst := &engineState{
		matchers:     make([]*Matcher, len(src.matchers)),
		matcherIndex: make(map[dto.EventType][]*Matcher, len(src.matcherIndex)),
		commandIndex: make(map[string]map[dto.EventType][]*Matcher, len(src.commandIndex)),
		groupIndex:   make(map[string][]*Matcher, len(src.groupIndex)),
		sortedCache:  make(map[dto.EventType][]*Matcher, len(src.sortedCache)),
		block:        src.block,
		maxMatchers:  src.maxMatchers,
	}

	// 复制 matchers 切片
	copy(dst.matchers, src.matchers)

	// 复制 matcherIndex map
	for k, v := range src.matcherIndex {
		dst.matcherIndex[k] = append([]*Matcher(nil), v...)
	}

	// 复制 commandIndex map (Deep copy)
	dst.commandIndex = make(map[string]map[dto.EventType][]*Matcher, len(src.commandIndex))
	for cmd, eventMap := range src.commandIndex {
		newEventMap := make(map[dto.EventType][]*Matcher, len(eventMap))
		for et, matchers := range eventMap {
			newEventMap[et] = append([]*Matcher(nil), matchers...)
		}
		dst.commandIndex[cmd] = newEventMap
	}

	// 复制 groupIndex map
	for k, v := range src.groupIndex {
		dst.groupIndex[k] = append([]*Matcher(nil), v...)
	}

	// 复制 sortedCache map
	for k, v := range src.sortedCache {
		dst.sortedCache[k] = append([]*Matcher(nil), v...)
	}

	return dst
}

// copyMiddlewareState 深拷贝中间件状态
func copyMiddlewareState(src *middlewareState) *middlewareState {
	dst := &middlewareState{
		global: middlewareSnapshot{
			chain: make([]Middleware, len(src.global.chain)),
			gen:   src.global.gen,
		},
		groupMiddlewares: make(map[string]*middlewareSnapshot, len(src.groupMiddlewares)),
		traceHook:        src.traceHook,
	}

	// 复制全局中间件
	copy(dst.global.chain, src.global.chain)

	// 复制分组中间件
	for k, v := range src.groupMiddlewares {
		snap := &middlewareSnapshot{
			chain: make([]Middleware, len(v.chain)),
			gen:   v.gen,
		}
		copy(snap.chain, v.chain)
		dst.groupMiddlewares[k] = snap
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

	// 重建索引
	for _, m := range s.matchers {
		cmd := m.GetCommand()
		if cmd != "" {
			if s.commandIndex[cmd] == nil {
				s.commandIndex[cmd] = make(map[dto.EventType][]*Matcher)
			}
			s.commandIndex[cmd][m.EventType] = append(s.commandIndex[cmd][m.EventType], m)
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
	} else {
		// 更新常规索引
		et := m.EventType
		s.matcherIndex[et] = append(s.matcherIndex[et], m)

		// 更新排序缓存
		matchers := s.matcherIndex[et]
		sorted := make([]*Matcher, len(matchers))
		copy(sorted, matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[et] = sorted
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

	// Filter s.matchers in place
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
		sorted := make([]*Matcher, len(matchers))
		copy(sorted, matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[eventType] = sorted
	} else {
		// 如果该类型没有 matchers，确保从 cache 中移除 (safe delete)
		delete(s.sortedCache, eventType)
	}

	// 如果是具体事件类型，也需要重建通用匹配器缓存
	if eventType != "" {
		if matchers, ok := s.matcherIndex[""]; ok {
			sorted := make([]*Matcher, len(matchers))
			copy(sorted, matchers)
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
