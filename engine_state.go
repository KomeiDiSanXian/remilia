package remilia

import (
	"sort"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// engineState 不可变的引擎状态（COW 模式）
//
// 此结构体包含所有需要在读操作中访问的状态。
// 写操作会复制整个状态，修改后原子替换。
type engineState struct {
	// 核心匹配器列表
	matchers []*Matcher

	// 匹配器索引（按事件类型分组）
	matcherIndex map[dto.EventType][]*Matcher

	// 排序缓存（按优先级排序的匹配器）
	sortedCache map[dto.EventType][]*Matcher

	// 配置项
	block       bool // 是否阻断后续匹配器
	maxMatchers int  // 最大匹配器数量限制
}

// middlewareSnapshot 表示不可变的中间件切片及其代际号
// gen 每次修改对应切片时递增，用于缓存失效
type middlewareSnapshot struct {
	chain []HandlerMiddleware
	gen   uint64
}

// middlewareState 不可变的中间件状态（COW 模式）
//
// 中间件配置独立于引擎状态，可以单独更新。
type middlewareState struct {
	// 全局中间件快照
	global middlewareSnapshot

	// 插件中间件快照（按插件名称分组）
	pluginMiddlewares map[string]*middlewareSnapshot

	// 中间件追踪开关
	traceEnabled bool
}

// newEngineState 创建新的引擎状态
func newEngineState() *engineState {
	return &engineState{
		matchers:     make([]*Matcher, 0),
		matcherIndex: make(map[dto.EventType][]*Matcher),
		sortedCache:  make(map[dto.EventType][]*Matcher),
		block:        false,
		maxMatchers:  0,
	}
}

// newMiddlewareState 创建新的中间件状态
func newMiddlewareState() *middlewareState {
	return &middlewareState{
		global:            middlewareSnapshot{chain: make([]HandlerMiddleware, 0), gen: 1},
		pluginMiddlewares: make(map[string]*middlewareSnapshot),
		traceEnabled:      false,
	}
}

// copyEngineState 深拷贝引擎状态
func copyEngineState(src *engineState) *engineState {
	dst := &engineState{
		matchers:     make([]*Matcher, len(src.matchers)),
		matcherIndex: make(map[dto.EventType][]*Matcher, len(src.matcherIndex)),
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
			chain: make([]HandlerMiddleware, len(src.global.chain)),
			gen:   src.global.gen,
		},
		pluginMiddlewares: make(map[string]*middlewareSnapshot, len(src.pluginMiddlewares)),
		traceEnabled:      src.traceEnabled,
	}

	// 复制全局中间件
	copy(dst.global.chain, src.global.chain)

	// 复制插件中间件
	for k, v := range src.pluginMiddlewares {
		snap := &middlewareSnapshot{
			chain: make([]HandlerMiddleware, len(v.chain)),
			gen:   v.gen,
		}
		copy(snap.chain, v.chain)
		dst.pluginMiddlewares[k] = snap
	}

	return dst
}

// rebuildIndex 重建匹配器索引和排序缓存
func (s *engineState) rebuildIndex() {
	// 清空旧索引
	s.matcherIndex = make(map[dto.EventType][]*Matcher)
	s.sortedCache = make(map[dto.EventType][]*Matcher)

	// 重建索引
	for _, m := range s.matchers {
		et := m.EventType
		s.matcherIndex[et] = append(s.matcherIndex[et], m)
	}

	// 重建排序缓存
	for eventType, matchers := range s.matcherIndex {
		sorted := make([]*Matcher, len(matchers))
		copy(sorted, matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[eventType] = sorted
	}
}

// addMatcher 添加匹配器到状态
func (s *engineState) addMatcher(m *Matcher) {
	s.matchers = append(s.matchers, m)

	// 更新索引
	et := m.EventType
	s.matcherIndex[et] = append(s.matcherIndex[et], m)

	// 更新排序缓存
	matchers := s.matcherIndex[et]
	sorted := make([]*Matcher, len(matchers))
	copy(sorted, matchers)
	sortMatchersByPriority(sorted)
	s.sortedCache[et] = sorted
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

// invalidateSortedCache 失效并重建指定事件类型的排序缓存
func (s *engineState) invalidateSortedCache(eventType dto.EventType) {
	// 重建指定事件类型的缓存
	if matchers, ok := s.matcherIndex[eventType]; ok {
		sorted := make([]*Matcher, len(matchers))
		copy(sorted, matchers)
		sortMatchersByPriority(sorted)
		s.sortedCache[eventType] = sorted
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
