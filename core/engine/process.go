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

// processEventGuard 是 ProcessEvent / ProcessPlatformEvent 的通用防护层：
// shutdown 检查、eventWg 计数、panic 恢复。
func (e *Engine) processEventGuard(ctx *context.Context) {
	e.processEventGuarded(ctx, true)
}

// processEventGuarded 是事件处理入口的公共防护层。
//
// allowPool 为 true 时慢 handler 可 offload 到 ExecPool（ProcessEvent 路径）；
// 为 false 时强制在当前 goroutine 执行（ProcessEventSync 路径）。
// 两条路径都必须经过 shutdown 检查、eventWg 计数与 panic 恢复：
// 否则 Shutdown 返回后仍会有 handler 在运行，规则/中间件构造中的 panic
// 也会直接抛给调用方。
func (e *Engine) processEventGuarded(ctx *context.Context, allowPool bool) {
	if e.shutdown.Load() {
		return
	}
	recordEventMetrics(ctx)

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

	if allowPool {
		e.processEventContextWithPool(ctx)
		return
	}

	ctx.SetDispatcher(e.dispatcher)
	e.processEventMatchers(ctx, false)
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
//     RLock 读取 Handler → buildHandlerChain（校验快照一致性后写入缓存）
//
//  3. 终止路径
//     deleted 或 handler == nil 时提前返回
//
// invokeRejectHandler 调用 matcher 声明的「忙时处理器」。
//
// 处理器的 panic 与普通 handler 一样会被隔离：它只是给用户一句说明，
// 不能让准入拒绝升级为进程级故障。
func (e *Engine) invokeRejectHandler(ctx *context.Context, m *Matcher) {
	m.rt.mu.RLock()
	h := m.rejectHandlerLocked()
	m.rt.mu.RUnlock()
	if h == nil {
		return
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WithFields(logger.Fields{
					"panic":   r,
					"matcher": m.sourceLocked(),
				}).Warn("[engine] Reject handler panic recovered")
			}
		}()
		if err := h(ctx); err != nil {
			logger.WithError(err).Debugf("[engine] Reject handler error in matcher: %s", m.sourceLocked())
		}
	}()
}

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

		// 确保中间件链是最新的（gen 不匹配时重建），并在写入缓存前
		// 校验快照未被并发中间件变更污染（见 buildHandlerChain）。
		finalHandler = e.buildHandlerChain(m, handlerErr)
	}

	recordHandlerError := func(err error) {
		logger.WithError(err).Debugf("[engine] Handler error in matcher: %s", m.sourceLocked())

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
				"matcher":    m.sourceLocked(),
				"event_type": ctx.GetEventType(),
			}).Error("[engine] Handler panic recovered")
			// panic 路径同样触发 metrics 记录
			recordHandlerError(panicErr)
		}
	}()

	err := finalHandler(ctx)

	// 准入/限流拒绝：被拒事件没有执行任何业务代码，不算 handler 错误；
	// 交给 matcher 声明的忙时处理器（插件决定文案），没有就静默结束。
	if context.IsBusy(err) {
		e.invokeRejectHandler(ctx, m)
		err = nil
	}

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
	// handler 自行 Delete() 的一次性 matcher：deleted 已置位导致上面的
	// useCount 分支被跳过，这里兜底从 TempManager 移除，避免临时 matcher
	// 残留为僵尸（GetTempMatcherCount 永不归零、HasAny 恒真）。
	remove := m.rt.deleted.Load()
	m.rt.mu.Unlock()

	if remove {
		e.internals.tempManager.Remove(m)
	}
}

// buildHandlerChain 构建 matcher 的中间件处理链并写入缓存。
//
// 事件协程可能与中间件变更（Use / UseForGroup / ResetMiddlewares）并发：
// 读到旧中间件状态后缓存又被失效（compiledVersion 自增、combinedChain 清空），
// 若此时用旧链填充缓存并把版本号写成失效后的新值，之后热路径会长期命中这份
// 旧链——鉴权、日志等中间件被整体跳过。
//
// 因此写入缓存前必须确认快照仍然有效：版本号未变，且链的代际号与当前中间件
// 状态一致。校验失败说明构建期间发生了并发变更，重新读取状态重试；连续失败时
// 退化为构建一次但不写缓存，保证本次事件仍使用最新中间件。
func (e *Engine) buildHandlerChain(m *Matcher, he context.Handler) context.Handler {
	for range maxHandlerChainBuildAttempts {
		mwState := e.middleware.Load()
		e.ensureMatcherChainWithState(m, mwState)
		chain, chainGlobalGen, chainGroupGen := m.getChainCache()
		cv := m.compiledVersion.Load()

		head := e.composeIterChain(chain, he)
		m.compiledHandlers.Store(&compiledChain{head: head, version: cv})

		if e.chainSnapshotValid(m, cv, chainGlobalGen, chainGroupGen) {
			return head
		}
	}

	// 极端情况下（构建期间持续发生中间件变更）不再写缓存，
	// 直接用当前状态构建，避免把过期链固化下来。
	mwState := e.middleware.Load()
	e.ensureMatcherChainWithState(m, mwState)
	chain, _, _ := m.getChainCache()
	return e.composeIterChain(chain, he)
}

// chainSnapshotValid 判断构建 handler 链时使用的快照是否仍然有效：
// compiledVersion 未变化，且组合链记录的中件间代际号与当前中间件状态一致。
func (e *Engine) chainSnapshotValid(m *Matcher, cv, chainGlobalGen, chainGroupGen uint64) bool {
	if m.compiledVersion.Load() != cv {
		return false
	}
	globalGen, groupGen := e.middlewareGensFor(m)
	return globalGen == chainGlobalGen && groupGen == chainGroupGen
}

// middlewareGensFor 返回 matcher 所属分组当前的全局/分组中间件代际号。
func (e *Engine) middlewareGensFor(m *Matcher) (globalGen, groupGen uint64) {
	mwState := e.middleware.Load()
	if mwState == nil {
		return 0, 0
	}
	if snap := mwState.groupMiddlewares[m.group]; snap != nil {
		groupGen = snap.gen
	}
	return mwState.global.gen, groupGen
}

// composeIterChain 把中间件链与业务 handler 组合成单一入口 handler。
//
// 从右向左构建，让每个中间件闭包捕获已构建好的 "next"：
//
//	tmp[N]   = he                       (actual handler)
//	tmp[N-1] = chain[N-1](tmp[N])       (innermost middleware)
//	...
//	tmp[0]   = chain[0](tmp[1])         (outermost middleware)
//
// 只返回 tmp[0]（入口）；tmp[1..N] 通过闭包捕获保持存活，不依赖切片。
func (e *Engine) composeIterChain(chain []context.Middleware, he context.Handler) context.Handler {
	// Fast path: no middleware → call handler directly, zero overhead.
	if len(chain) == 0 {
		return he
	}

	tmp := make([]context.Handler, len(chain)+1)
	tmp[len(chain)] = he
	for i := len(chain) - 1; i >= 0; i-- {
		tmp[i] = chain[i](tmp[i+1])
	}

	return tmp[0]
}

// maxHandlerChainBuildAttempts 限制 buildHandlerChain 在并发中间件变更下的重试次数。
const maxHandlerChainBuildAttempts = 4

// startPendingDeleteProcessor 启动批量删除处理器
//
// 从 pendingDeleteCh 通道中批量删除匹配器。
// 处理间隔取 WithPendingDeleteProcessInterval 配置值（修复：此前误用常量，
// 自定义间隔被忽略），批量大小为 pendingDeleteBatchSize。
func (e *Engine) startPendingDeleteProcessor() func() {
	interval := e.internals.pendingDeleteProcessInterval
	if interval <= 0 {
		interval = DefaultPendingDeleteProcessInterval
	}
	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	e.internals.pendingDeleteDone = done
	e.internals.pendingDeleteActive.Store(true)

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

	// 返回停止函数（幂等；停止后 DeleteMatcher 回退为同步删除）
	var once sync.Once
	return func() {
		once.Do(func() {
			e.internals.pendingDeleteActive.Store(false)
			close(done)
		})
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
		e.internals.tempMatcherCleanerStop = e.startTempMatcherCleanerLocked(interval)
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
	// 公开入口统一加 writeMu：tempMatcherCleanerStop/Done 也会被
	// Shutdown 路径（tempCleanerComponent.stop/wait）在 writeMu 下访问，
	// 无锁写入会在并发调用时产生双清理器/泄漏的竞态。
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	return e.startTempMatcherCleanerLocked(interval)
}

// startTempMatcherCleanerLocked 是 StartTempMatcherCleaner 的内部实现，
// 调用方必须已持有 e.writeMu。
func (e *Engine) startTempMatcherCleanerLocked(interval time.Duration) func() {
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
	// CleanExpired 内部已统一标记 deleted 并重建 RCU 快照
	tempExpired := e.internals.tempManager.CleanExpired()
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
