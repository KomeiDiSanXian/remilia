package engine

import (
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ProcessEvent 处理事件（COW 无锁读取）。
//
// 慢 handler 自动 offload 到 ExecPool，池满时 fallback 同步。
func (e *Engine) ProcessEvent(ctx *context.Context) {
	e.processEventGuard(ctx)
}

// processEventGuard 是 ProcessEvent 和 ProcessPlatformEvent 的通用防护层：
// shutdown 检查、eventWg 计数、panic 恢复。
func (e *Engine) processEventGuard(ctx *context.Context) {
	if e.shutdown.Load() {
		return
	}

	e.eventMu.Lock()
	if e.shutdown.Load() {
		e.eventMu.Unlock()
		return
	}
	e.eventWg.Add(1)
	e.eventMu.Unlock()
	defer e.eventWg.Done()

	defer func() {
		if r := recover(); r != nil {
			logger.WithFields(logger.Fields{
				"panic":      r,
				"stack":      string(debug.Stack()),
				"event_type": ctx.GetEventType(),
				"platform":   ctx.GetPlatformEvent(),
			}).Error("[engine] Unhandled panic in event processing recovered")
		}
	}()

	e.processEventContextWithPool(ctx)
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
	if cc := m.compiledHandlers.Load(); cc != nil && cc.version == cv {
		finalHandler = cc.head
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

		if mc := e.internals.metricsCollector.Load(); mc != nil {
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

	// 临时 matcher：按使用次数自动删除。
	// Fast path: non-temp matchers (the vast majority) skip the write-lock entirely.
	if atomic.LoadInt32(&m.rt.isTemp) == 0 {
		return
	}
	m.rt.mu.Lock()
	if m.rt.maxUseCount > 0 && !m.rt.deleted.Load() {
		m.rt.useCount++
		if m.rt.useCount >= m.rt.maxUseCount {
			m.rt.deleted.Store(true)
			m.rt.mu.Unlock()

			e.internals.tempManager.Remove(m)
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
	cv := m.compiledVersion.Load()

	if cc := m.compiledHandlers.Load(); cc != nil && cc.version == cv {
		return cc.head
	}

	// Fast path: no middleware → call handler directly, zero overhead.
	// Still cache in compiledHandlers so invokeHandler's fast path skips
	// the entire ensureMatcherChainWithState + getChainCache on every event.
	if len(chain) == 0 {
		m.compiledHandlers.Store(&compiledChain{head: he, version: cv})
		return he
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
	e.internals.pendingDeleteDone = done

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
	limit := e.internals.pendingDeleteBatchSize // 每次最多处理

loop:
	for range limit {
		select {
		case m := <-e.internals.pendingDeleteCh:
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
	if e.internals.tempMatcherCleanerStop != nil {
		e.internals.tempMatcherCleanerStop()
		e.internals.tempMatcherCleanerStop = nil
	}

	e.internals.tempMatcherCleanerInterval = interval
	if interval > 0 {
		e.internals.tempMatcherCleanerStop = e.StartTempMatcherCleaner(interval)
	}

	return e
}

// GetTempMatcherCleanInterval 获取当前的临时 Matcher 清理间隔
func (e *Engine) GetTempMatcherCleanInterval() time.Duration {
	return e.internals.tempMatcherCleanerInterval
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
	if e.internals.tempMatcherCleanerStop != nil {
		e.internals.tempMatcherCleanerStop()
		e.internals.tempMatcherCleanerStop = nil
	}

	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	e.internals.tempMatcherCleanerDone = done
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
	e.internals.tempMatcherCleanerStop = stopFn
	return stopFn
}

// cleanExpiredMatchers 清理过期的临时 matcher（COW 无锁读取 + TempManager 堆）
func (e *Engine) cleanExpiredMatchers() {
	// 1. 清理 TempManager 中的过期 matcher (高效堆实现)
	tempExpired := e.internals.tempManager.CleanExpired()
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


