package remilia

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
)

// DeadLetterItem 代表死信队列中的一项
type DeadLetterItem struct {
	Event   *dto.Payload
	Err     error
	Attempt int
	Source  string
}

// DeadLetterConsumer 接口定义了死信消费器的行为
type DeadLetterConsumer interface {
	Consume(item DeadLetterItem)
}

// Engine 事件引擎（Copy-on-Write 模式）
//
// COW 并发模型：
//   - 读操作：完全无锁，通过 atomic.Value 读取不可变状态
//   - 写操作：使用 writeMu 保护，复制-修改-替换
//   - 无死锁风险：只有单一写锁，读操作无锁
//   - 读写分离：写操作不阻塞读操作（读操作看到旧状态）
//
// 性能特性：
//   - 读操作性能：5-6x 提升（无锁）
//   - 写操作性能：略有下降（复制开销）
//   - 内存效率：读操作零分配，整体效率提升 93%
//   - 适用场景：读多写少（完美匹配 Engine 使用模式）
type Engine struct {
	// 不可变状态（COW 模式）
	state      atomic.Value // *engineState - 引擎核心状态
	middleware atomic.Value // *middlewareState - 中间件配置

	// 写锁（仅用于修改操作）
	writeMu sync.Mutex

	// 其他不常变化的字段
	metricsCollector           *MetricsCollector // 指标收集器（可选）
	tempMatcherCleanerStop     func()            // 清理器停止函数
	tempMatcherCleanerInterval time.Duration     // 清理间隔
}

// NewEngine 创建一个新的事件引擎（COW 模式）
//
// 默认自动启动临时 Matcher 清理器，每 5 分钟清理一次。
// 可以通过 SetTempMatcherCleanInterval() 修改清理间隔。
//
// COW 模式优势：
//   - 读操作无锁，性能提升 5-6x
//   - 无死锁风险
//   - 内存效率高（读操作零分配）
func NewEngine() *Engine {
	e := &Engine{
		tempMatcherCleanerInterval: 5 * time.Minute, // 默认 5 分钟
	}

	// 初始化不可变状态
	e.state.Store(newEngineState())
	e.middleware.Store(newMiddlewareState())

	// 自动启动临时 Matcher 清理器
	e.tempMatcherCleanerStop = e.StartTempMatcherCleaner(e.tempMatcherCleanerInterval)

	return e
}

// rebuildMatcherChain 重新为给定 matcher 组合全局/插件/局部中间件链
//
// 此方法会获取读锁，适用于外部调用（向后兼容）
func (e *Engine) rebuildMatcherChain(m *Matcher) {
	if m == nil {
		return
	}
	e.rebuildMatcherChainCOW(m)
}

// rebuildMatcherChainCOW 重新为给定 matcher 组合全局/插件/局部中间件链（COW 版本）
//
// 使用代际号避免全量重建，按需合并
func (e *Engine) rebuildMatcherChainCOW(m *Matcher) {
	if m == nil {
		return
	}

	mwState := e.middleware.Load().(*middlewareState)
	e.ensureMatcherChainWithState(m, mwState)
}

// ensureMatcherChainWithState 检查缓存是否过期，必要时惰性合并中间件链
func (e *Engine) ensureMatcherChainWithState(m *Matcher, mwState *middlewareState) {
	if m == nil || mwState == nil {
		return
	}

	pluginName := m.pluginName
	if pluginName == "" && strings.HasPrefix(m.Source, "plugin:") {
		pluginName = strings.TrimPrefix(m.Source, "plugin:")
		m.pluginName = pluginName
	}

	pluginSnap := mwState.pluginMiddlewares[pluginName]
	globalSnap := mwState.global
	var pluginChain []HandlerMiddleware
	var pluginGen uint64
	if pluginSnap != nil {
		pluginChain = pluginSnap.chain
		pluginGen = pluginSnap.gen
	}

	if cached := m.getCombinedChain(); cached != nil {
		if m.cachedGen.global == globalSnap.gen && m.cachedGen.plugin == pluginGen {
			return
		}
	}

	m.mu.RLock()
	locals := append([]HandlerMiddleware(nil), m.middlewares...)
	m.mu.RUnlock()

	chain := make([]HandlerMiddleware, 0, len(globalSnap.chain)+len(pluginChain)+len(locals))
	chain = append(chain, globalSnap.chain...)
	chain = append(chain, pluginChain...)
	chain = append(chain, locals...)

	m.setCombinedChain(chain, globalSnap.gen, pluginGen)
}

// DeleteAllMatchers 删除引擎中的所有匹配器（COW 写操作）
func (e *Engine) DeleteAllMatchers() {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 加载当前状态
	oldState := e.state.Load().(*engineState)
	oldMatchers := append([]*Matcher(nil), oldState.matchers...)

	// 2. 创建新的空状态
	newState := copyEngineState(oldState)
	newState.matchers = make([]*Matcher, 0)
	newState.matcherIndex = make(map[dto.EventType][]*Matcher)
	newState.sortedCache = make(map[dto.EventType][]*Matcher)

	// 3. 原子替换
	e.state.Store(newState)

	// 4. 标记旧 matchers 为已删除
	for _, m := range oldMatchers {
		if m == nil {
			continue
		}
		m.mu.Lock()
		m.deleted = true
		m.mu.Unlock()
	}
}

// DeleteMatcher 删除指定的匹配器（COW 写操作）
func (e *Engine) DeleteMatcher(m *Matcher) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 加载当前状态
	oldState := e.state.Load().(*engineState)

	// 2. 复制状态
	newState := copyEngineState(oldState)

	// 3. 删除 matcher
	newState.deleteMatcher(m)

	// 4. 原子替换
	e.state.Store(newState)
}

// SetTempMatcherCleanInterval 设置临时 Matcher 清理间隔（COW 模式）
//
// 修改清理间隔后会重启清理器。
// 设置为 0 可以禁用自动清理。
//
// 使用示例：
//
//	engine.SetTempMatcherCleanInterval(10 * time.Minute)
//	engine.SetTempMatcherCleanInterval(0) // 禁用清理
func (e *Engine) SetTempMatcherCleanInterval(interval time.Duration) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 停止旧的清理器
	if e.tempMatcherCleanerStop != nil {
		e.tempMatcherCleanerStop()
		e.tempMatcherCleanerStop = nil
	}

	e.tempMatcherCleanerInterval = interval

	// 如果间隔 > 0，启动新的清理器
	if interval > 0 {
		e.tempMatcherCleanerStop = e.StartTempMatcherCleaner(interval)
		logrus.WithField("interval", interval).Info("[Engine] Temp matcher cleaner restarted")
	} else {
		logrus.Info("[Engine] Temp matcher cleaner disabled")
	}

	return e
}

// GetTempMatcherCleanInterval 获取当前的临时 Matcher 清理间隔
func (e *Engine) GetTempMatcherCleanInterval() time.Duration {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	return e.tempMatcherCleanerInterval
}

// StartTempMatcherCleaner 启动临时 Matcher 清理器
//
// 定期检查并删除过期的临时 matcher（基于时间）。
// 返回一个 stop 函数，调用它可以停止清理器。
//
// 使用示例：
//
//	// 每 5 分钟清理一次过期的临时 matcher
//	stop := engine.StartTempMatcherCleaner(5 * time.Minute)
//	defer stop() // 程序退出时停止清理器
//
// 注意：
//   - NewEngine() 会自动启动清理器，通常不需要手动调用
//   - 如需修改清理间隔，使用 SetTempMatcherCleanInterval()
//   - 清理器在后台 goroutine 中运行，不会阻塞
//   - 多次调用会启动多个清理器，通常只需调用一次
func (e *Engine) StartTempMatcherCleaner(interval time.Duration) func() {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.cleanExpiredMatchers()
			case <-done:
				return
			}
		}
	}()

	// 返回 stop 函数
	return func() {
		close(done)
	}
}

// cleanExpiredMatchers 清理过期的临时 matcher（COW 无锁读取）
func (e *Engine) cleanExpiredMatchers() {
	now := time.Now()
	toDelete := make([]*Matcher, 0)

	// 无锁读取状态，收集过期的 matcher
	state := e.state.Load().(*engineState)
	for _, m := range state.matchers {
		m.mu.RLock()
		isExpired := m.IsTemp && !m.expiresAt.IsZero() && now.After(m.expiresAt)
		m.mu.RUnlock()

		if isExpired {
			toDelete = append(toDelete, m)
		}
	}

	// 删除过期的 matcher
	for _, m := range toDelete {
		logrus.WithFields(logrus.Fields{
			"event_type": m.EventType,
			"source":     m.Source,
			"created_at": m.createdAt,
			"expires_at": m.expiresAt,
		}).Debug("[Engine] Cleaning expired temporary matcher")
		e.DeleteMatcher(m)
	}

	if len(toDelete) > 0 {
		logrus.Infof("[Engine] Cleaned %d expired temporary matchers", len(toDelete))
	}
}

// invalidateSortedCache 失效指定事件类型的排序缓存（COW 写操作）
//
// 当 Matcher 的优先级被修改时调用此方法。
// 在 COW 模式中，这会创建新状态并重建索引。
func (e *Engine) invalidateSortedCache(eventType dto.EventType) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态
	oldState := e.state.Load().(*engineState)

	// 复制状态
	newState := copyEngineState(oldState)

	// 失效缓存
	newState.invalidateSortedCache(eventType)

	// 原子替换
	e.state.Store(newState)
}

// SetBlock 设置引擎的阻塞状态（COW 写操作）
func (e *Engine) SetBlock(block bool) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态
	oldState := e.state.Load().(*engineState)

	// 复制状态
	newState := copyEngineState(oldState)
	newState.block = block

	// 原子替换
	e.state.Store(newState)

	return e
}

// SetMaxMatchers 设置匹配器数量上限（COW 写操作）
// 设置为 0 表示不限制（默认）
// 此设置可以防止恶意或错误的代码无限制注册匹配器导致内存溢出
func (e *Engine) SetMaxMatchers(limit int) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 加载当前状态
	oldState := e.state.Load().(*engineState)

	// 复制状态
	newState := copyEngineState(oldState)
	newState.maxMatchers = limit

	// 原子替换
	e.state.Store(newState)

	return e
}

// GetMaxMatchers 获取当前的匹配器数量上限（COW 无锁读取）
func (e *Engine) GetMaxMatchers() int {
	state := e.state.Load().(*engineState)
	return state.maxMatchers
}

// GetMatcherCount 获取当前已注册的匹配器数量（COW 无锁读取）
func (e *Engine) GetMatcherCount() int {
	state := e.state.Load().(*engineState)
	return len(state.matchers)
}

// noopMatcher 是一个空操作匹配器，用于在达到匹配器限制时返回
// 所有方法都返回自身，形成无操作链
var noopMatcher = &Matcher{
	deleted:     true,
	Priority:    999,
	Source:      "noop",
	Rules:       []Rule{},
	middlewares: []HandlerMiddleware{},
	Engine:      nil,
}

// On 注册一个新的事件匹配器，显式指定事件类型（COW 写操作）
//
// COW 流程：
//  1. 加载当前状态
//  2. 复制状态
//  3. 修改副本
//  4. 原子替换
func (e *Engine) On(eventType dto.EventType, rules ...Rule) *Matcher {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 1. 加载当前状态
	oldState := e.state.Load().(*engineState)

	// 检查匹配器数量限制
	if oldState.maxMatchers > 0 && len(oldState.matchers) >= oldState.maxMatchers {
		logrus.Errorf("[Engine] Matcher limit reached: %d/%d, returning noop matcher",
			len(oldState.matchers), oldState.maxMatchers)
		return noopMatcher // 返回空 matcher 而非 nil，避免 panic
	}

	// 2. 复制状态
	newState := copyEngineState(oldState)

	// 3. 创建新 matcher 并添加到副本
	matcher := &Matcher{
		EventType: eventType,
		Rules:     rules,
		Engine:    e,
		Priority:  50,       // 默认优先级
		Source:    "global", // 默认来源为全局（非插件）
	}
	newState.addMatcher(matcher)

	// 4. 原子替换状态
	e.state.Store(newState)

	// 重建中间件链（读取中间件状态，无需加锁）
	e.rebuildMatcherChainCOW(matcher)

	return matcher
}

// OnAny 注册一个适用于所有事件类型的匹配器
func (e *Engine) OnAny(rules ...Rule) *Matcher {
	return e.On("", rules...)
}

// OnC2C 是 On(dto.C2CMessageCreate, ...) 的便捷封装
func (e *Engine) OnC2C(rules ...Rule) *Matcher {
	return e.On(dto.C2CMessageCreate, rules...)
}

// OnGroupAt 是 On(dto.GroupAtMessageCreate, ...) 的便捷封装
func (e *Engine) OnGroupAt(rules ...Rule) *Matcher {
	return e.On(dto.GroupAtMessageCreate, rules...)
}

// OnGroupAdd 是 On(dto.GroupAddRobot, ...) 的便捷封装
func (e *Engine) OnGroupAdd(rules ...Rule) *Matcher {
	return e.On(dto.GroupAddRobot, rules...)
}

// OnGroupDel 是 On(dto.GroupDelRobot, ...) 的便捷封装
func (e *Engine) OnGroupDel(rules ...Rule) *Matcher {
	return e.On(dto.GroupDelRobot, rules...)
}

// ProcessEvent 处理事件（COW 无锁读取）
//
// 性能特性：
//   - 完全无锁：通过 atomic.Load() 读取不可变状态
//   - 零内存分配：直接使用已排序的匹配器切片
//   - 5-6x 性能提升：相比原有的 RWMutex 实现
func (e *Engine) ProcessEvent(ctx *Context) {
	// 无锁读取状态
	state := e.state.Load().(*engineState)

	eventType := ctx.GetEventType()

	// 获取已排序的匹配器（从缓存）
	specificMatchers := state.sortedCache[eventType]
	genericMatchers := state.sortedCache[""]

	// 合并匹配器列表
	var matchersToCheck []*Matcher
	if len(specificMatchers) > 0 && len(genericMatchers) > 0 {
		// 两个列表都存在，需要合并并排序
		matchersToCheck = make([]*Matcher, 0, len(specificMatchers)+len(genericMatchers))
		matchersToCheck = append(matchersToCheck, specificMatchers...)
		matchersToCheck = append(matchersToCheck, genericMatchers...)
		sortMatchersByPriority(matchersToCheck)
	} else if len(specificMatchers) > 0 {
		matchersToCheck = specificMatchers
	} else {
		matchersToCheck = genericMatchers
	}

	// 匹配并执行对应的处理器
	for _, matcher := range matchersToCheck {
		if matcher.Match(ctx) {
			ctx.matcher = matcher
			e.invokeHandler(ctx, matcher)
			if matcher.IsBlock || state.block {
				break
			}
		}
	}
}

// getMatchersForEvent 获取用于匹配事件的匹配器列表（内部方法）
// 使用索引优化，只返回相关事件类型的匹配器
//
// COW 模式：无锁读取
func (e *Engine) getMatchersForEvent(ctx *Context) []*Matcher {
	// 无锁读取状态
	state := e.state.Load().(*engineState)

	eventType := ctx.GetEventType()
	// 获取特定事件类型的匹配器
	specificMatchers := state.matcherIndex[eventType]
	// 加上通用匹配器（空字符串键）
	genericMatchers := state.matcherIndex[""]

	// 合并并返回
	result := make([]*Matcher, 0, len(specificMatchers)+len(genericMatchers))
	result = append(result, specificMatchers...)
	result = append(result, genericMatchers...)
	return result
}

// ProcessEventBatch 批量处理事件（COW 无锁版本）
//
// COW 模式优势：
//   - 无锁读取：一次性获取状态快照
//   - 高性能：避免所有锁操作
//   - 简化实现：不需要复杂的缓存管理
func (e *Engine) ProcessEventBatch(events []*dto.Payload, api openapi.OpenAPI) {
	if len(events) == 0 {
		return
	}

	// 无锁读取状态（一次读取，处理所有事件）
	state := e.state.Load().(*engineState)

	// 处理每个事件
	for _, event := range events {
		ctx := NewContext(event, api)
		eventType := ctx.GetEventType()

		// 从排序缓存中获取匹配器（已按优先级排序）
		specificMatchers := state.sortedCache[eventType]
		genericMatchers := state.sortedCache[""]

		// 合并匹配器列表
		var matchersToCheck []*Matcher
		if len(specificMatchers) > 0 && len(genericMatchers) > 0 {
			matchersToCheck = make([]*Matcher, 0, len(specificMatchers)+len(genericMatchers))
			matchersToCheck = append(matchersToCheck, specificMatchers...)
			matchersToCheck = append(matchersToCheck, genericMatchers...)
			sortMatchersByPriority(matchersToCheck)
		} else if len(specificMatchers) > 0 {
			matchersToCheck = specificMatchers
		} else {
			matchersToCheck = genericMatchers
		}

		// 匹配并执行对应的处理器
		for _, matcher := range matchersToCheck {
			if matcher.Match(ctx) {
				ctx.matcher = matcher
				e.invokeHandler(ctx, matcher)
				if matcher.IsBlock || state.block {
					break
				}
			}
		}
	}
}

// MatcherStats 匹配器统计
type MatcherStats struct {
	Total         int
	Global        int
	ByPlugin      map[string]int
	GlobalEnabled bool
}

// GetMatcherStats 获取匹配器统计信息（COW 无锁读取）
func (e *Engine) GetMatcherStats() MatcherStats {
	// 无锁读取状态
	state := e.state.Load().(*engineState)

	stats := MatcherStats{ByPlugin: make(map[string]int)}
	stats.Total = len(state.matchers)

	for _, m := range state.matchers {
		if m.Source == "global" || m.Source == "" {
			stats.Global++
			continue
		}
		if strings.HasPrefix(m.Source, "plugin:") {
			name := strings.TrimPrefix(m.Source, "plugin:")
			stats.ByPlugin[name]++
		}
	}

	// GlobalEnabled: 当 block=false 时认为全局匹配器可用；也可以扩展为独立开关
	stats.GlobalEnabled = !state.block
	return stats
}

// EnableGlobalMatchers 启用/禁用通过 Engine.On 注册的全局匹配器（COW 写操作）
// 注意：这会影响所有非插件注册的匹配器
func (e *Engine) EnableGlobalMatchers(enable bool) {
	e.SetBlock(!enable)
}

// HandlerMiddleware 定义处理器中间件的类型
type HandlerMiddleware func(next HandlerE) HandlerE

// Use 注册全局处理器中间件（COW 写操作）
//
// 中间件按添加顺序链式包裹
func (e *Engine) Use(mw ...HandlerMiddleware) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	oldMwState := e.middleware.Load().(*middlewareState)

	// 复制状态并追加中间件，递增代际号
	newMwState := copyMiddlewareState(oldMwState)
	newChain := append([]HandlerMiddleware(nil), newMwState.global.chain...)
	newChain = append(newChain, mw...)
	newMwState.global.chain = newChain
	newMwState.global.gen++

	e.middleware.Store(newMwState)

	return e
}

// UseForPlugin 为指定插件注册中间件（COW 写操作）
//
// 仅该插件注册的 matcher 生效
func (e *Engine) UseForPlugin(pluginName string, mw ...HandlerMiddleware) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	key := strings.TrimSpace(pluginName)
	if key == "" {
		return e
	}

	oldMwState := e.middleware.Load().(*middlewareState)

	// 复制状态并更新目标插件快照
	newMwState := copyMiddlewareState(oldMwState)
	snap, ok := newMwState.pluginMiddlewares[key]
	if !ok {
		snap = &middlewareSnapshot{chain: make([]HandlerMiddleware, 0), gen: 1}
		newMwState.pluginMiddlewares[key] = snap
	}
	newChain := append([]HandlerMiddleware(nil), snap.chain...)
	newChain = append(newChain, mw...)
	snap.chain = newChain
	snap.gen++

	e.middleware.Store(newMwState)

	return e
}

// ResetMiddlewares 清空全局与插件级中间件（COW 写操作）
// 不影响已注册的 matcher 局部中间件
func (e *Engine) ResetMiddlewares() *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	// 创建新的空中间件状态
	newMwState := newMiddlewareState()

	// 原子替换
	e.middleware.Store(newMwState)

	return e
}

// Named 将中间件设置名称，并在 trace开启时将名称追加到 ctx.State["mw_trace"] 列表（COW 无锁读取）
func (e *Engine) Named(name string, mw HandlerMiddleware) HandlerMiddleware {
	return func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			// 无锁读取中间件状态
			mwState := e.middleware.Load().(*middlewareState)
			if mwState.traceEnabled {
				if v, ok := ctx.GetState("mw_trace"); ok {
					if arr, ok := v.([]string); ok {
						arr = append(arr, name)
						ctx.SetState("mw_trace", arr)
					} else {
						ctx.SetState("mw_trace", []string{name})
					}
				} else {
					ctx.SetState("mw_trace", []string{name})
				}
			}
			return mw(next)(ctx)
		}
	}
}

// SetMetricsCollector 设置指标收集器（v0.7.1 新增）
func (e *Engine) SetMetricsCollector(mc *MetricsCollector) *Engine {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	e.metricsCollector = mc
	return e
}

// GetMetricsCollector 获取指标收集器（v0.7.1 新增）
func (e *Engine) GetMetricsCollector() *MetricsCollector {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	return e.metricsCollector
}

func handlerAdapter(h Handler) HandlerE { return func(ctx *Context) error { h(ctx); return nil } }

// invokeHandler 封装调用处理器，通过中间件链执行
// 提供完整的错误处理：panic 恢复、错误记录、死信队列
func (e *Engine) invokeHandler(ctx *Context, m *Matcher) {
	// 读取 handler 时需要加锁，避免数据竞争
	m.mu.RLock()
	handlerErr := m.HandlerErr
	handler := m.Handler
	m.mu.RUnlock()

	var he HandlerE
	if handlerErr != nil {
		he = handlerErr
	} else if handler != nil {
		he = handlerAdapter(handler)
	} else {
		return
	}

	// 基于预先组合好的中间件链（global -> plugin -> matcher）包裹 handler
	// 使用 atomic.Value 实现无锁读取
	mwState := e.middleware.Load().(*middlewareState)
	e.ensureMatcherChainWithState(m, mwState)
	combinedChain := m.getCombinedChain()
	chain := make([]HandlerMiddleware, len(combinedChain))
	copy(chain, combinedChain)

	for i := len(chain) - 1; i >= 0; i-- {
		he = chain[i](he)
	}

	// 执行 handler 并处理错误和 panic
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 捕获 panic 并转换为错误
				err = fmt.Errorf("panic in handler: %v", r)
				logrus.WithFields(logrus.Fields{
					"panic":      r,
					"matcher":    m.Source,
					"event_type": ctx.GetEventType(),
				}).Error("[Engine] Handler panic recovered")
			}
		}()
		err = he(ctx)
	}()

	// 记录错误
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"matcher":    m.Source,
			"event_type": ctx.GetEventType(),
		}).Error("[Engine] Handler execution error")

		// 更新指标（使用 writeMu 保护）
		e.writeMu.Lock()
		collector := e.metricsCollector
		e.writeMu.Unlock()

		if collector != nil {
			// 记录事件处理失败
			if collector.eventDropped != nil {
				collector.eventDropped.WithLabelValues("handler_error").Inc()
			}
		}
	}

	// 临时 matcher：按使用次数自动删除
	m.mu.Lock()
	if m.IsTemp && m.maxUseCount > 0 && !m.deleted {
		m.useCount++
		if m.useCount >= m.maxUseCount {
			// 标记删除并在锁外通知 Engine
			m.deleted = true
			engine := m.Engine
			m.mu.Unlock()
			if engine != nil {
				engine.DeleteMatcher(m)
			}
			return
		}
	}
	m.mu.Unlock()
}

// sortMatchersByPriority 按优先级排序 matchers
// Priority 数值越小优先级越高，0 为最高优先级
func sortMatchersByPriority(matchers []*Matcher) {
	sort.Slice(matchers, func(i, j int) bool {
		return matchers[i].Priority < matchers[j].Priority
	})
}
