package engine

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ProcessEvent 处理事件（COW 无锁读取）
//
// 性能特性：
//   - 完全无锁：通过 atomic.Load() 读取不可变状态
//   - 零内存分配：直接使用已排序的匹配器切片（通过 sync.Pool）
//   - 5-6x 性能提升：相比原有的 RWMutex 实现
func (e *Engine) ProcessEvent(ctx *context.Context) {
	// 用读锁保护"检查 shutdown → eventWg.Add(1)"的原子性。
	// Shutdown() 持有写锁完成 shutdown.Store(true) 后立即释放，
	// 随后调用 eventWg.Wait()，此时已无法再进入此 RLock 区间并执行 Add(1)。
	e.shutdownMu.RLock()
	if e.shutdown.Load() {
		e.shutdownMu.RUnlock()
		return
	}
	e.eventWg.Add(1)
	e.shutdownMu.RUnlock()
	defer e.eventWg.Done()

	// 顶层 panic 保护，防止任何未捕获的 panic 导致 goroutine 崩溃
	defer func() {
		if r := recover(); r != nil {
			logger.WithFields(logger.Fields{
				"panic":      r,
				"event_type": ctx.GetEventType(),
			}).Error("[engine] Unhandled panic in ProcessEvent recovered")
		}
	}()

	e.processEventContext(ctx)
}

// invokeHandler 封装调用处理器，通过中间件链执行
// 提供完整的错误处理：panic 恢复、错误记录、死信队列
//
// 性能优化：使用预编译迭代器链替代逐次构建嵌套闭包。
// 中间件链在首次调用（或链变化）时编译为 []Handler，后续调用直接迭代，
// 消除每次 invocation 产生 len(chain) 个闭包的堆分配。
func (e *Engine) invokeHandler(ctx *context.Context, m *Matcher) {
	// 读取 handler 时需要加锁，避免数据竞争
	m.rt.mu.RLock()
	if m.rt.deleted {
		m.rt.mu.RUnlock()
		return
	}
	handlerErr := m.Handler
	m.rt.mu.RUnlock()

	if handlerErr == nil {
		return
	}

	// 确保中间件链是最新的
	mwState := e.middleware.Load()
	e.ensureMatcherChainWithState(m, mwState)
	chain, _, _ := m.getChainCache()

	// 获取或构建预编译迭代器链
	finalHandler := e.getOrBuildIterChain(m, chain, handlerErr)
	recordHandlerError := func(err error) {
		logger.WithError(err).Debugf("[engine] Handler error in matcher: %s", m.Source)

		if mc := e.services.metricsCollector.Load(); mc != nil {
			mc.RecordEventDropped("handler_error")
		}
	}

	// 执行 handler 并处理 panic
	var panicErr error
	defer func() {
		if r := recover(); r != nil {
			panicErr = fmt.Errorf("panic in handler: %v", r)
			logger.WithFields(logger.Fields{
				"panic":      r,
				"matcher":    m.Source,
				"event_type": ctx.GetEventType(),
			}).Error("[engine] Handler panic recovered")
			// panic 路径同样触发 metrics 记录
			recordHandlerError(panicErr)
		}
	}()

	err := finalHandler(ctx)

	// 记录正常路径错误
	if err != nil {
		recordHandlerError(err)
	}

	// 临时 matcher：按使用次数自动删除
	m.rt.mu.Lock()
	if atomic.LoadInt32(&m.rt.isTemp) == 1 && m.rt.maxUseCount > 0 && !m.rt.deleted {
		m.rt.useCount++
		if m.rt.useCount >= m.rt.maxUseCount {
			m.rt.deleted = true
			isTemp := atomic.LoadInt32(&m.rt.isTemp) == 1
			m.rt.mu.Unlock()

			engine := e
			if isTemp {
				engine.services.tempManager.Remove(m)
			} else {
				select {
				case engine.services.pendingDeleteCh <- m:
				default:
					logger.Debugf("[engine] Pending delete channel full, matcher %p (source: %s) marked for cleanup", m, m.Source)
				}
			}
			return
		}
	}
	m.rt.mu.Unlock()
}

// getOrBuildIterChain returns a single Handler that, when called, executes
// the full middleware chain iteratively (no nested closures on hot path).
//
// The compiled chain is cached in m.compiledHandlers and is keyed by the
// identity of both the core handler and the middleware slice length.
// When either changes the cache is rebuilt (rare: only on middleware
// registration or handler hot-reload), so the fast path is alloc-free.
func (e *Engine) getOrBuildIterChain(m *Matcher, chain []context.Middleware, he context.Handler) context.Handler {
	// Fast path: no middleware → call handler directly, zero overhead.
	if len(chain) == 0 {
		return he
	}

	hid := handlerID(he)
	sig := chainSignature(chain)

	if v := m.compiledHandlers.Load(); v != nil {
		if cc, ok := v.(*compiledChain); ok && cc != nil {
			// Valid if same handler identity AND same middleware content fingerprint.
			// chainSig catches same-length-but-different-content replacements that
			// the old length-only check would miss.
			if cc.handlerSig == hid && cc.chainSig == sig {
				return cc.handlers[0]
			}
		}
	}

	// Slow path: build the compiled chain once.
	// handlers[i] = chain[i](handlers[i+1]): each is a single closure
	// allocated here, not on every invocation.
	handlers := make([]context.Handler, len(chain)+1)
	handlers[len(chain)] = he
	for i := len(chain) - 1; i >= 0; i-- {
		handlers[i] = chain[i](handlers[i+1])
	}

	cc := &compiledChain{
		handlers:   handlers,
		handlerSig: hid,
		chainSig:   sig,
	}
	m.compiledHandlers.Store(cc)
	return handlers[0]
}

// chainSignature computes an order-sensitive fingerprint of all middleware
// function pointers in the chain using FNV-1a chained hashing.
//
// Unlike XOR-based fingerprints, this is ORDER-SENSITIVE:
//   - chain [A, B] and [B, A] produce different signatures
//   - chain [A, A] and [A] produce different signatures
//
// This ensures that reordering middleware correctly invalidates the compiled
// chain cache, preventing stale handler chains from being used.
func chainSignature(chain []context.Middleware) uint64 {
	const (
		fnvOffset uint64 = 0xcbf29ce484222325
		fnvPrime  uint64 = 0x100000001b3
	)
	h := fnvOffset
	for _, m := range chain {
		ptr := uint64(reflect.ValueOf(m).Pointer())
		h = (h ^ ptr) * fnvPrime
	}
	return h
}

// handlerID returns a stable numeric identity for a Handler function value
// using reflect. Two calls with the same closure object return the same id.
func handlerID(h context.Handler) uintptr {
	if h == nil {
		return 0
	}
	return reflect.ValueOf(h).Pointer()
}

// startPendingDeleteProcessor 启动批量删除处理器
//
// 从 pendingDeleteCh 通道中批量删除匹配器。
// 每个批量处理间隔为 DefaultPendingDeleteProcessInterval，批量大小为 DefaultPendingDeleteBatchSize。
func (e *Engine) startPendingDeleteProcessor() func() {
	ticker := time.NewTicker(DefaultPendingDeleteProcessInterval)
	done := make(chan struct{})

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.processPendingDeletes()
			case <-done:
				return
			}
		}
	}()

	// 返回停止函数
	return func() {
		close(done)
	}
}

// processPendingDeletes 处理待删除的匹配器（批量删除）
func (e *Engine) processPendingDeletes() {
	var matchersToDelete []*Matcher
	limit := DefaultPendingDeleteBatchSize // 每次最多处理

loop:
	for range limit {
		select {
		case m := <-e.services.pendingDeleteCh:
			if m != nil {
				matchersToDelete = append(matchersToDelete, m)
			}
		default:
			break loop
		}
	}

	if len(matchersToDelete) > 0 {
		e.DeleteMatchers(matchersToDelete)
	}
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

	// Stop existing cleaner if running
	if e.services.tempMatcherCleanerStop != nil {
		e.services.tempMatcherCleanerStop()
		e.services.tempMatcherCleanerStop = nil
	}

	e.services.tempMatcherCleanerInterval = interval
	if interval > 0 {
		e.services.tempMatcherCleanerStop = e.StartTempMatcherCleaner(interval)
	}

	return e
}

// GetTempMatcherCleanInterval 获取当前的临时 Matcher 清理间隔
func (e *Engine) GetTempMatcherCleanInterval() time.Duration {
	return e.services.tempMatcherCleanerInterval
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
	e.services.tempMatcherCleanerDone = done
	var once sync.Once

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
		once.Do(func() {
			close(done)
		})
	}
}

// cleanExpiredMatchers 清理过期的临时 matcher（COW 无锁读取 + TempManager 堆）
func (e *Engine) cleanExpiredMatchers() {
	// 1. 清理 TempManager 中的过期 matcher (高效堆实现)
	tempExpired := e.services.tempManager.CleanExpired()
	for _, m := range tempExpired {
		m.rt.mu.Lock()
		m.rt.deleted = true
		m.rt.mu.Unlock()
	}
	if len(tempExpired) > 0 {
		logger.Debugf("[engine] Cleaned %d temp matchers from TempManager", len(tempExpired))
	}
}

// extractCommand 提取消息中的命令词（首个空格前的单词）
// 自动去除首尾空白，确保 "  /ping  " 也能正确提取为 "/ping"
func extractCommand(content string) string {
	trimmed := strings.TrimSpace(content)
	idx := strings.IndexFunc(trimmed, unicode.IsSpace)
	if idx == -1 {
		return trimmed
	}
	return trimmed[:idx]
}

// mergeSortedMatchersSix 将 6 个**已按优先级排序**的 Matcher 子列表合并到 dst 中，
// 输出结果同样按优先级升序排列（数值越小优先级越高）。
//
// # 6 路由两个维度组合而来：
//
//	维度 1 — 事件类型范围
//	  Specific：EventType 与当前事件匹配的 Matcher（精确匹配，优先于通配）
//	  Generic ：EventType == "" 的 Matcher（通配，匹配任意事件）
//
//	维度 2 — 存储位置
//	  State (perm)：通过 registerMatcher 注册到 COW state 的永久 Matcher
//	  State (cmd) ：同上但命中了 commandIndex 快速路径（已有 /cmd 前缀索引）
//	  Temp        ：通过 OnTemp 注册到 TempManager 的临时 Matcher
//
//	                 Specific          Generic
//	  State(perm)    l1=permSpecific   l4=permGeneric
//	  State(cmd)     l2=cmdSpecific    l5=cmdGeneric
//	  Temp           l3=tempSpecific   l6=tempGeneric
//
// # 合并算法
//
// 各子列表在进入此函数前已经按优先级排好序（由 sortMatchersByPriority 保证），
// 因此这里**不是归并排序**，而是一个 6 路"选最小堆头"的线性合并：
//  1. 对每个列表找到第一个"有效头"（跳过迁移到 Temp/State 的陈旧元素）
//  2. 从所有有效头中选出优先级最小的，追加到 dst
//  3. 重复直到所有列表耗尽
//
// 时间复杂度：O(N × 6) = O(N)，其中 N 为所有列表元素总和；6 为常数。
//
// # isStateSource 标志
//
// State 列表（l1/l2/l4/l5）中可能残留已被迁移到 TempManager 的元素（isTemp==1），
// 需要跳过（避免双重执行）。Temp 列表（l3/l6）反之。
func mergeSortedMatchersSix(dst []*Matcher, l1, l2, l3, l4, l5, l6 []*Matcher) []*Matcher {
	totalLen := len(l1) + len(l2) + len(l3) + len(l4) + len(l5) + len(l6)
	// 优化：所有列表为空时直接返回
	if totalLen == 0 {
		return dst[:0]
	}

	if cap(dst) < totalLen {
		dst = make([]*Matcher, 0, totalLen)
	}
	dst = dst[:0]

	// Indices
	idx := [6]int{0, 0, 0, 0, 0, 0}
	lens := [6]int{len(l1), len(l2), len(l3), len(l4), len(l5), len(l6)}
	lists := [6][]*Matcher{l1, l2, l3, l4, l5, l6}
	// true if sourced from State (skip if isTemp), false if sourced from Temp (skip if !isTemp)
	isStateSource := [6]bool{true, true, false, true, true, false}

	for {
		// 1. Advance indices if current head items should be skipped
		stop := true
		for k := range 6 {
			for idx[k] < lens[k] {
				m := lists[k][idx[k]]
				isTemp := atomic.LoadInt32(&m.rt.isTemp) == 1
				shouldSkip := false
				if isStateSource[k] {
					if isTemp {
						shouldSkip = true
					}
				} else {
					if !isTemp {
						shouldSkip = true
					}
				}

				if shouldSkip {
					idx[k]++
				} else {
					stop = false
					break // Valid head found for list k
				}
			}
		}

		if stop {
			break
		}

		// 2. Find min priority among valid heads
		minP := uint(999999999)
		winner := -1

		for k := range 6 {
			if idx[k] < lens[k] {
				// At this point, lists[k][idx[k]] is a VALID candidate
				p := lists[k][idx[k]].getPriority()
				if p < minP {
					minP = p
					winner = k
				}
			}
		}

		if winner != -1 {
			dst = append(dst, lists[winner][idx[winner]])
			idx[winner]++
		}
	}
	return dst
}

// setContextMatcher 设置 context 的 matcher 字段（解决跨包访问问题）
func setContextMatcher(ctx *context.Context, m *Matcher) {
	// 使用 Extensions 存储 matcher 信息
	type matcherInfo struct {
		Source string
	}
	context.ExtSet(ctx.Ext(), matcherInfo{Source: m.Source})
}
