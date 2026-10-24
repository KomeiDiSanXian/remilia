package engine

// testhelpers_test.go — 测试辅助（事件桩 + 仅测试可达的内部操作）
//
// 提供两部分内容，全部只在 _test.go 中可见：
//  1. engineTestEvent / newEngineForTest：engine 包测试共用的事件桩与
//     Engine 构造器（迁移自旧路径 dto.Payload 测试）。
//  2. 状态与处理链的测试专用操作：clone / addMatcher / removeGroup /
//     deleteMatcher(s) / deleteFromSlice / getOrBuildIterChain /
//     Matcher.copy / Matcher.setCombinedChain。
//
// 第 2 部分此前留在 state.go / matcher.go / process.go 里，生产路径已改走
// 选择性 COW 的 with* 方法（withAddedMatcher / withRemovedGroup /
// withDeletedMatcher / withDeletedMatchers）与 buildHandlerChain，只剩单元测试
// 在调用。放进 _test.go 后生产二进制不再包含这些死代码，测试也无需重写。
//
// 新增测试辅助请一律加在本文件，不要加回生产文件。

import (
	"context"
	"maps"
	"sync/atomic"
	"testing"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// engineTestEvent 是 platform.Event 的测试桩，供 engine 包测试使用。
type engineTestEvent struct {
	kind     platform.EventKind
	content  string
	senderID string
	chatID   string
	rawType  string
	id       string
}

func (e *engineTestEvent) Platform() string         { return "test" }
func (e *engineTestEvent) Kind() platform.EventKind { return e.kind }
func (e *engineTestEvent) RawType() string          { return e.rawType }

func (e *engineTestEvent) Segments() []platform.Segment {
	if e.content == "" {
		return nil
	}
	return []platform.Segment{{Type: platform.SegmentText, Text: e.content}}
}
func (e *engineTestEvent) Chat() platform.ChatInfo   { return platform.ChatInfo{ID: e.chatID} }
func (e *engineTestEvent) Sender() platform.UserInfo { return platform.UserInfo{ID: e.senderID} }
func (e *engineTestEvent) Timestamp() time.Time      { return time.Time{} }
func (e *engineTestEvent) ID() string                { return e.id }
func (e *engineTestEvent) RawPayload() any           { return nil }

// newTestPlatformEvent 创建指定 EventKind 的测试桩事件。
func newTestPlatformEvent(kind platform.EventKind) platform.Event {
	return &engineTestEvent{
		kind:     kind,
		content:  "test message",
		senderID: "sender-001",
		chatID:   "chat-001",
		rawType:  string(kind),
	}
}

// newTestPlatformEventWithContent 创建指定 EventKind 和消息内容的测试桩事件。
func newTestPlatformEventWithContent(kind platform.EventKind, content string) platform.Event {
	return &engineTestEvent{
		kind:     kind,
		content:  content,
		senderID: "sender-001",
		chatID:   "chat-001",
		rawType:  string(kind),
	}
}

// newEngineForTest 创建一个 Engine 并绑定测试生命周期。
//
// 默认禁用后台工作者（temp cleaner / pending delete），避免 goroutine 泄漏。
// 测试结束时自动调用 Shutdown。同时适用于 *testing.T 和 *testing.B。
func newEngineForTest(t testing.TB, opts ...Option) *Engine {
	t.Helper()
	e := NewEngine(append([]Option{WithNoBackgroundWorkers(), WithExecPoolDisabled()}, opts...)...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Shutdown(ctx)
	})
	return e
}

// ============================================================================
// 仅测试可达的内部操作（生产代码使用 with* / buildHandlerChain）
// ============================================================================

// clone 完整深拷贝所有 map（仅测试用）。
func (s *state) clone() *state {
	dst := &state{
		matchers:         s.matchers[:len(s.matchers):len(s.matchers)],
		matcherIndex:     copyMatcherIndex(s.matcherIndex),
		commandIndex:     copyCommandIndex(s.commandIndex),
		regexIndex:       copyRegexIndex(s.regexIndex),
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

// addMatcher 添加匹配器到状态（仅测试用，生产代码使用 withAddedMatcher）。
func (s *state) addMatcher(m *Matcher) {
	s.matchers = append(s.matchers, m)

	// 自动推导 hasHandler：直接设置 Handler 字段（非 Handle() 调用）也生效
	if m.handlerLocked() != nil {
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
	} else if m.regexIndexed.Load() {
		if m.handlerLocked() != nil { // 正则桶按 Handler 过滤（见 withAddedMatcher）
			s.regexIndex[m.EventType] = append(s.regexIndex[m.EventType], m)
			sortMatchersByPriority(s.regexIndex[m.EventType])
		}
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

// removeGroup 移除指定分组的所有匹配器（仅测试用，生产代码使用 withRemovedGroup）。
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

// deleteMatcher 从状态中删除匹配器（仅测试用，生产代码使用 withDeletedMatcher）。
func (s *state) deleteMatcher(m *Matcher) {
	s.matchers = deleteFromSlice(s.matchers, m)
	s.rebuildIndex()
}

// deleteMatchers 从状态中批量删除匹配器（仅测试用，生产代码使用 withDeletedMatchers）。
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

// deleteFromSlice 删除切片中第一个匹配的元素（测试辅助）。
func deleteFromSlice[T comparable](slice []T, elem T) []T {
	for i, v := range slice {
		if v == elem {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// getOrBuildIterChain 构建/复用迭代式中间件链（仅测试用）。
//
// 生产路径走 buildHandlerChain（process.go），它在写缓存前校验中间件快照，
// 避免并发变更把旧链固化下来；本函数保留原语义供单测验证组合逻辑。
func (e *Engine) getOrBuildIterChain(m *Matcher, chain []corectx.Middleware, he corectx.Handler) corectx.Handler {
	cv := m.compiledVersion.Load()

	if cc := m.compiledHandlers.Load(); cc != nil && cc.version == cv {
		return cc.head
	}

	head := e.composeIterChain(chain, he)
	m.compiledHandlers.Store(&compiledChain{head: head, version: cv})
	return head
}

// copy 深拷贝匹配器（仅测试用）。
func (m *Matcher) copy() *Matcher {
	// 规则走 COW 快照（不会被就地修改，可在锁外读取）；
	// 其余可变字段统一在锁下取快照：并发 Handle/SetSource 可能正在改写它们。
	rules := m.ruleSnapshot()
	m.rt.mu.RLock()
	middlewares := m.middlewares
	handler := m.Handler
	source := m.Source
	group := m.group
	definition := m.definition
	triggerPrefix := m.triggerPrefix
	regexPattern := m.regexPattern
	rt := matcherRuntime{
		isTemp:      atomic.LoadInt32(&m.rt.isTemp),
		useCount:    m.rt.useCount,
		maxUseCount: m.rt.maxUseCount,
		createdAt:   m.rt.createdAt,
		expiresAt:   m.rt.expiresAt,
	}
	m.rt.mu.RUnlock()

	newRules := append([]corectx.Rule(nil), rules...)
	newMiddlewares := append([]corectx.Middleware(nil), middlewares...)

	newM := &Matcher{
		rt: matcherRuntime{
			isTemp:      rt.isTemp,
			useCount:    rt.useCount,
			maxUseCount: rt.maxUseCount,
			createdAt:   rt.createdAt,
			expiresAt:   rt.expiresAt,
		},
		EventType:     m.EventType,
		Rules:         newRules,
		Handler:       handler,
		Source:        source,
		group:         group,
		middlewares:   newMiddlewares,
		definition:    definition,
		triggerPrefix: triggerPrefix,
		execProfile:   m.execProfile,
	}
	newM.priority.Store(m.priority.Load())
	newM.isBlock.Store(m.isBlock.Load())
	newM.commandIndexed.Store(m.commandIndexed.Load())
	newM.regexIndexed.Store(m.regexIndexed.Load())
	newM.regexPattern = regexPattern
	newM.hasHandler.Store(m.hasHandler.Load())
	return newM
}

// setCombinedChain 设置组合的中间件链（仅测试用）。
func (m *Matcher) setCombinedChain(chain []corectx.Middleware, globalGen, groupGen uint64) {
	m.cacheMu.Lock()
	m.cachedGen.global = globalGen
	m.cachedGen.group = groupGen
	m.combinedChain.Store(chain)
	m.cacheMu.Unlock()
}
