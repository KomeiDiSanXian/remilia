package engine

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ProcessEvent 处理事件（COW 无锁读取）
func (e *Engine) ProcessEvent(ctx *context.Context) {
	// 无锁门控：CAS 递增活跃计数；若已 shutdown 则直接返回
	if !e.gate.acquire() {
		return
	}
	defer e.gate.release()

	// 顶层 panic 保护，防止任何未捕获的 panic 导致 goroutine 崩溃。
	// 同时覆盖 ProcessPlatformEvent 通过委托调用此函数的场景：
	// ctx.GetEventPlatform() 在平台路径下返回平台标识，非平台路径返回 ""。
	defer func() {
		if r := recover(); r != nil {
			logger.WithFields(logger.Fields{
				"panic":      r,
				"event_type": ctx.GetEventType(),
				"platform":   ctx.GetEventPlatform(),
			}).Error("[engine] Unhandled panic in event processing recovered")
		}
	}()

	e.processEventContext(ctx)
}

// invokeHandler 封装调用处理器，通过中间件链执行
// 提供完整的错误处理：panic 恢复、错误记录、死信队列
//
// 三级调用路径（由快到慢）：
//
//  1. 超高速路径（稳定态，99.9% 的调用）
//     deleted.Load → compiledVersion.Load → compiledHandlers.Load → 版本比对
//     → 直接返回 cc.handlers[0]；零锁、零函数调用、零 reflect。
//
//  2. 慢速路径（链首次编译或链/handler 变更后）
//     RLock 读取 Handler → ensureChain → getOrBuildIterChain
//
//  3. 终止路径
//     deleted 或 handler == nil 时提前返回
func (e *Engine) invokeHandler(ctx *context.Context, m *Matcher) {
	// ── 快速终止：atomic deleted 检查，无需任何锁 ──────────────────────────────
	if m.rt.deleted.Load() {
		return
	}

	// ── 超高速路径：编译好的 handler 链版本匹配 ─────────────────────────────────
	// 在稳定态（中间件和 handler 不变）下，99.9% 的调用走这里：
	//   1 次 atomic.Uint64.Load（compiledVersion）
	//   1 次 atomic.Value.Load（compiledHandlers）
	//   1 次整数比较
	// 完全零锁、零 reflect、零额外函数调用。
	var finalHandler context.Handler
	cv := m.compiledVersion.Load()
	if v := m.compiledHandlers.Load(); v != nil {
		if cc, ok := v.(*compiledChain); ok && cc != nil && cc.version == cv {
			finalHandler = cc.head
		}
	}

	// ── 慢速路径：链首次构建 或 handler/middleware 变更后重建 ──────────────────
	if finalHandler == nil {
		// 读取 handler 时需要加锁，避免数据竞争
		m.rt.mu.RLock()
		if m.rt.deleted.Load() {
			m.rt.mu.RUnlock()
			return
		}
		handlerErr := m.Handler
		m.rt.mu.RUnlock()

		if handlerErr == nil {
			return
		}

		// 确保中间件链是最新的（gen 不匹配时重建）
		mwState := e.middleware.Load()
		e.ensureMatcherChainWithState(m, mwState)
		chain, _, _ := m.getChainCache()
		finalHandler = e.getOrBuildIterChain(m, chain, handlerErr)
	}

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
	// Fast path: non-temp matchers (the vast majority) skip the write-lock entirely.
	if atomic.LoadInt32(&m.rt.isTemp) == 0 {
		return
	}
	m.rt.mu.Lock()
	if atomic.LoadInt32(&m.rt.isTemp) == 1 && m.rt.maxUseCount > 0 && !m.rt.deleted.Load() {
		m.rt.useCount++
		if m.rt.useCount >= m.rt.maxUseCount {
			m.rt.deleted.Store(true)
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
// 性能优化（版本计数器代替 reflect 指纹）：
//
// 原实现每次调用都执行 handlerID(reflect.ValueOf.Pointer) + chainSignature
// (len(chain) 次 reflect.ValueOf.Pointer)，在 1000-matcher 场景下产生
// ~11 000 次 reflect 调用/事件，占总 CPU 时间约 21%。
//
// 新实现改为比较 m.compiledVersion（atomic.Uint64 计数器）：
//   - Fast path（99.9% of calls）：1 次 atomic.Load + 1 次整数比较，零 reflect 调用。
//   - Slow path（链或 handler 变化后首次调用）：才真正重建并缓存。
//
// m.compiledVersion 在以下时机递增：
//   - invalidateCombinedChain()：中间件链重建时
//   - ensureChain()：combined chain 实际更新时
//   - Handle()：handler 更换时
func (e *Engine) getOrBuildIterChain(m *Matcher, chain []context.Middleware, he context.Handler) context.Handler {
	// Fast path: no middleware → call handler directly, zero overhead.
	// No compiledChain stored: the handler itself is the final value,
	// and it is returned directly from the slow path on every call.
	if len(chain) == 0 {
		return he
	}

	cv := m.compiledVersion.Load()

	if v := m.compiledHandlers.Load(); v != nil {
		if cc, ok := v.(*compiledChain); ok && cc != nil && cc.version == cv {
			return cc.head
		}
	}

	// Slow path: compose the middleware chain once and cache only the head.
	//
	// We build right-to-left using a temporary local slice so each closure
	// can capture the already-built "next" value:
	//
	//   tmp[N]   = he                       (actual handler)
	//   tmp[N-1] = chain[N-1](tmp[N])       (innermost middleware)
	//   ...
	//   tmp[0]   = chain[0](tmp[1])         (outermost middleware)
	//
	// Only tmp[0] (head) is stored in compiledChain; tmp[1..N] are kept alive
	// through the closure capture chain, not through the slice.
	tmp := make([]context.Handler, len(chain)+1)
	tmp[len(chain)] = he
	for i := len(chain) - 1; i >= 0; i-- {
		tmp[i] = chain[i](tmp[i+1])
	}

	cc := &compiledChain{
		head:    tmp[0],
		version: cv,
	}
	m.compiledHandlers.Store(cc)
	return tmp[0]
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

// StartTempMatcherCleaner 启动临时 Matcher 清理器。
//
// 定期检查并删除过期的临时 matcher（基于时间）。
// 返回一个 stop 函数，调用它可以停止清理器。
//
// 幂等保护：若已有清理器在运行，会先停止旧的再以新间隔启动新的，
// 保证任意时刻最多只有一个清理器 goroutine，防止两个清理器并发处理同一批过期 Matcher。
//
// 使用示例：
//
//	// 每 5 分钟清理一次过期的临时 matcher
//	stop := engine.StartTempMatcherCleaner(5 * time.Minute)
//	defer stop() // 程序退出时停止清理器
//
// 注意：
//   - NewEngine() 会自动启动清理器，通常不需要手动调用此方法
//   - 如需修改清理间隔，请优先使用 SetTempMatcherCleanInterval()
//   - 清理器在后台 goroutine 中运行，不会阻塞
func (e *Engine) StartTempMatcherCleaner(interval time.Duration) func() {
	// 防重复启动：若旧清理器仍在运行，先停止它再启动新的，
	// 确保任意时刻最多只有一个清理器 goroutine 在运行。
	if e.services.tempMatcherCleanerStop != nil {
		e.services.tempMatcherCleanerStop()
		e.services.tempMatcherCleanerStop = nil
	}

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

	// 返回 stop 函数，并将其注册到 services，供下次调用时幂等停止
	stopFn := func() {
		once.Do(func() {
			close(done)
		})
	}
	e.services.tempMatcherCleanerStop = stopFn
	return stopFn
}

// cleanExpiredMatchers 清理过期的临时 matcher（COW 无锁读取 + TempManager 堆）
func (e *Engine) cleanExpiredMatchers() {
	// 1. 清理 TempManager 中的过期 matcher (高效堆实现)
	tempExpired := e.services.tempManager.CleanExpired()
	for _, m := range tempExpired {
		m.rt.deleted.Store(true)
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
// 因此这里不是归并排序，而是一个 6 路"选最小堆头"的线性合并：
//  1. 对每个列表找到第一个"有效头"（跳过 isTemp 状态不匹配的陈旧元素）
//  2. 从所有有效头中选出优先级最小的 winner
//  3. 对 winner 做一次 TOCTOU 二次确认；若已变陈旧则跳过并重试
//  4. 重复直到所有列表耗尽
//
// 时间复杂度：O(N × 6) = O(N)，其中 N 为所有列表元素总和；6 为常数。
//
// # atomic.LoadInt32 次数分析
//
// 每次外层迭代（最多 N 次）：
//   - Phase 1：每个未耗尽列表的头部 1 次 Load（共 ≤6 次）
//   - Phase 2：不再对非 winner 执行 Load，仅对 winner 做 1 次 TOCTOU 确认
//
// 相比旧实现（Phase 1 ≤6 次 + Phase 2 ≤6 次 + fallback ≤6 次），
// 每次外层迭代最多节省 5 次 LoadInt32；N=1000 时节省 ~5000 次原子读。
//
// # isStateSource 标志
//
// State 列表（l1/l2/l4/l5）中可能残留已被迁移到 TempManager 的元素（isTemp==1），
// 需要跳过（避免双重执行）。Temp 列表（l3/l6）反之。
func mergeSortedMatchersSix(dst []*Matcher, l1, l2, l3, l4, l5, l6 []*Matcher) []*Matcher {
	totalLen := len(l1) + len(l2) + len(l3) + len(l4) + len(l5) + len(l6)
	if totalLen == 0 {
		return dst[:0]
	}

	if cap(dst) < totalLen {
		dst = make([]*Matcher, 0, totalLen)
	}
	dst = dst[:0]

	idx := [6]int{}
	lens := [6]int{len(l1), len(l2), len(l3), len(l4), len(l5), len(l6)}
	lists := [6][]*Matcher{l1, l2, l3, l4, l5, l6}
	// true → State source（skip when isTemp==1）；false → Temp source（skip when isTemp==0）
	isStateSource := [6]bool{true, true, false, true, true, false}

	for {
		// ── Phase 1：推进各列表，跳过陈旧头部 ──────────────────────────────────────
		// 每个非耗尽列表仅做 1 次 atomic.LoadInt32，与旧实现相同。
		stop := true
		for k := range 6 {
			for idx[k] < lens[k] {
				isTemp := atomic.LoadInt32(&lists[k][idx[k]].rt.isTemp) == 1
				if (isStateSource[k] && isTemp) || (!isStateSource[k] && !isTemp) {
					idx[k]++ // stale — skip
				} else {
					stop = false
					break // 找到列表 k 的有效头部
				}
			}
		}

		if stop {
			break
		}

		// ── Phase 2：选出最低优先级的 winner ────────────────────────────────────────
		// 各列表头部在 Phase 1 中已确认有效，此处直接按 priority 比较，
		// 不对非 winner 执行 atomic.LoadInt32（相比旧实现节省 ≤5 次/迭代）。
		minP := uint(999999999)
		winner := -1
		for k := range 6 {
			if idx[k] < lens[k] {
				if p := lists[k][idx[k]].getPriority(); p < minP {
					minP = p
					winner = k
				}
			}
		}

		// stop==false 保证至少一个列表非空，winner 必然 ≥ 0；此处仅作防御性保护。
		if winner == -1 {
			break
		}

		// ── TOCTOU 确认：仅对 winner 做一次二次 Load ─────────────────────────────
		// 若 winner 在 Phase 1 与 Phase 2 之间发生 isTemp 迁移，则跳过并重试。
		// 非 winner 即使发生迁移，下次外层迭代的 Phase 1 会自动处理。
		//
		// 正确性：
		//  - 若 winner 仍有效 → emit，继续。
		//  - 若 winner 变陈旧 → advance，外层循环重试；其他候选不受影响。
		//  - 若非 winner 变陈旧且优先级更高（数值更小）→ 该非 winner 被错误选为 winner
		//    → TOCTOU 检测到陈旧 → advance → 重试 → 真正 winner 在下次迭代被选中。
		isTemp2 := atomic.LoadInt32(&lists[winner][idx[winner]].rt.isTemp) == 1
		if (isStateSource[winner] && isTemp2) || (!isStateSource[winner] && !isTemp2) {
			idx[winner]++ // winner 已失效，跳过并重试
			continue
		}

		dst = append(dst, lists[winner][idx[winner]])
		idx[winner]++
	}
	return dst
}
