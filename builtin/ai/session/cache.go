// cache.go — 会话级缓存：动作选择结果、历史检索结果、工具集稳定状态。
//
// 三者都是"按会话复用上一轮计算结果"的槽位，生命周期与 Session 一致
// （json:"-" 不持久化，重启后重新收敛），因此随 Session 一并归位。
//
// 本文件只保存槽位数据与访问器；缓存的复用策略（TTL / 相似度门槛）属于
// 消费方领域，定义在 builtin/ai/retrieval（见 CacheReuseTTL / CacheReuseJaccard），
// 会话层不反向依赖动作/工具类型。
package session

import (
	"maps"
	"time"
)

// SelectionCache 会话级工具选择缓存（json:"-" 不持久化）。
type SelectionCache struct {
	QueryTokens map[string]float64
	At          time.Time
	ToolCount   int
	Names       []string
}

// ToolSelection 返回会话缓存的工具选择结果副本（无缓存返回 nil）。
// 返回副本而非内部指针，避免调用方在锁外读写 QueryTokens/Names 与
// SetToolSelection 的并发写入竞争（与 RAGCacheSnapshot / ToolSetState 一致）。
func (s *Session) ToolSelection() *SelectionCache {
	s.Lock()
	defer s.Unlock()
	return s.selCache.clone()
}

// clone 深拷贝选择缓存（跨锁返回，避免调用方在锁外读写共享字段）。
func (c *SelectionCache) clone() *SelectionCache {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Names = append([]string(nil), c.Names...)
	if len(c.QueryTokens) > 0 {
		cp.QueryTokens = make(map[string]float64, len(c.QueryTokens))
		maps.Copy(cp.QueryTokens, c.QueryTokens)
	} else {
		cp.QueryTokens = nil
	}
	return &cp
}

// SetToolSelection 写入会话工具选择缓存。
func (s *Session) SetToolSelection(c *SelectionCache) {
	s.Lock()
	defer s.Unlock()
	s.selCache = c
}

// RAGCache 会话级历史检索缓存（json:"-" 不持久化）。
// 缓存完整注入文本：命中即零工作（含无结果的空缓存）。
type RAGCache struct {
	QueryTokens map[string]float64
	At          time.Time
	ChatKey     string
	Text        string
}

// RAGCacheSnapshot 返回会话缓存的检索结果副本（无缓存返回 nil）。
func (s *Session) RAGCacheSnapshot() *RAGCache {
	s.Lock()
	defer s.Unlock()
	if s.ragCache == nil {
		return nil
	}
	cp := *s.ragCache
	cp.QueryTokens = make(map[string]float64, len(s.ragCache.QueryTokens))
	maps.Copy(cp.QueryTokens, s.ragCache.QueryTokens)
	return &cp
}

// SetRAGCache 写入会话检索缓存。
func (s *Session) SetRAGCache(c *RAGCache) {
	s.Lock()
	defer s.Unlock()
	s.ragCache = c
}

// ToolSetState 会话级工具集稳定状态（json:"-" 不持久化，重启后重新收敛）。
type ToolSetState struct {
	// Names 当前稳定集合的工具名。始终按名称升序保存，与 ToolRegistry.List
	// 的排序一致——集合不变时序列化结果逐字节相同，这是前缀缓存的前提。
	Names []string
	// LastSeen 各工具最近一次进入检索候选的时间，用于补充项排序与空闲衰减。
	LastSeen map[string]time.Time
	// Generation 变更代数：集合每变化一次 +1，用于观测抖动频率。
	Generation uint64
	// ChangedAt 最近一次变更时间。
	ChangedAt time.Time
}

// clone 深拷贝状态（跨锁返回，避免调用方在锁外读写共享字段）。
func (st *ToolSetState) clone() *ToolSetState {
	if st == nil {
		return nil
	}
	cp := &ToolSetState{
		Names:      append([]string(nil), st.Names...),
		Generation: st.Generation,
		ChangedAt:  st.ChangedAt,
	}
	if len(st.LastSeen) > 0 {
		cp.LastSeen = make(map[string]time.Time, len(st.LastSeen))
		maps.Copy(cp.LastSeen, st.LastSeen)
	}
	return cp
}

// ToolSetState 返回会话工具集状态的副本（无状态返回 nil）。
func (s *Session) ToolSetState() *ToolSetState {
	s.Lock()
	defer s.Unlock()
	return s.toolSet.clone()
}

// SetToolSetState 写入会话工具集状态。
func (s *Session) SetToolSetState(st *ToolSetState) {
	s.Lock()
	defer s.Unlock()
	s.toolSet = st
}
