package engine

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ExecPool 是有界 goroutine 池，用于执行慢 handler。
//
// 设计原则：
//   - 不阻塞调用方（TrySubmit 非阻塞）
//   - 池满时返回 false，调用方应 fallback 到同步执行
//   - 支持 Drain 用于优雅关闭
//
// 区别于传统的 work stealing：我们的场景中所有任务都是 I/O 密集的
// （第三方 API 调用），不是 CPU 密集的。work stealing 的优势
// （缓存亲和、负载均衡）在此场景下不显著。有界令牌池 + 有界队列
// 已足够。
type ExecPool struct {
	sem     chan struct{} // 并发令牌，容量 = maxConcurrency
	queue   chan func()   // 有界等待队列，容量 = queueSize
	stopped atomic.Bool
	// inflight 统计「已成功提交、尚未执行完」的任务。
	// 用 drainTracker 而不是 sync.WaitGroup：TrySubmit 与 Drain 天然并发，
	// WaitGroup 归零后再 Add 会触发 "reused before previous Wait has returned"。
	inflight drainTracker

	// gate 把「复查 stopped + 登记在途」与 Drain 串行化（见 TrySubmit 说明）。
	gate sync.RWMutex

	// exitMu 串行化「worker 归还令牌前的最终空队列检查」与「入队后确保存在消费者」
	// 两个动作，消除任务在两者间隙入队后无人消费的滞留窗口。
	// 临界区内只有非阻塞操作，不影响直接执行的快路径。
	exitMu sync.Mutex
}

// ExecPoolConfig 是 ExecPool 的配置参数。
type ExecPoolConfig struct {
	MaxConcurrency int // 最大并发 goroutine 数（默认 64）
	QueueSize      int // 等待队列大小（默认 128）
}

// DefaultExecPoolConfig 返回默认配置。
func DefaultExecPoolConfig() ExecPoolConfig {
	return ExecPoolConfig{
		MaxConcurrency: 64,
		QueueSize:      128,
	}
}

// NewExecPool 创建有界 goroutine 池。
//
// 池中的 goroutine 是 lazily 创建的——只有任务到达时才创建，
// 通过 sem channel 限制最大并发数。空闲时 goroutine 退出。
func NewExecPool(cfg ExecPoolConfig) *ExecPool {
	if cfg.MaxConcurrency == 0 {
		return nil
	}
	if cfg.MaxConcurrency < 0 {
		cfg.MaxConcurrency = 64
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 128
	}
	return &ExecPool{
		sem:   make(chan struct{}, cfg.MaxConcurrency),
		queue: make(chan func(), cfg.QueueSize),
	}
}

// Wait 等待所有已提交的任务完成（不停止池，测试用）。
//
// 仅在测试中需要确保异步 handler 完成时使用。
func (p *ExecPool) Wait() {
	_ = p.inflight.Wait(context.Background())
}

// TrySubmit 尝试提交任务到池中执行。
//
//   - 如果池中有空闲 slot，立即启动 goroutine 执行。
//   - 如果池满但有队列空间，任务排队等待。
//   - 如果池满且队列满，返回 false（调用方应 fallback 同步执行）。
//
// 队列停滞预防：
//
//	worker 在归还令牌前的最终空队列检查与 TrySubmit 的入队动作由 exitMu
//	串行化：入队成功后要么仍有 worker 持有令牌（其退出检查必然看到该任务），
//	要么能立刻抢到令牌并启动一个 drain worker——任务不会滞留到下一次提交。
//
// 关闭栅栏：gate 读锁覆盖「复查 stopped + 登记在途 + 入队/起 worker」，
// Drain 置 stopped 后取写锁，因此 Drain 不会在一个已通过检查、尚未登记
// 的任务之前返回（否则任务会在 Drain 返回后才执行）。
func (p *ExecPool) TrySubmit(task func()) bool {
	p.gate.RLock()
	if p.stopped.Load() {
		p.gate.RUnlock()
		return false
	}

	// 尝试获取令牌（非阻塞）
	select {
	case p.sem <- struct{}{}:
		// 获取到令牌，启动 goroutine
		p.inflight.Add(1)
		p.gate.RUnlock()
		go p.runWorker(task)
		return true
	default:
		// 池满，尝试入队
	}

	// 尝试入队（非阻塞）。与 worker 的退出协议互斥（见 exitMu 注释）。
	p.exitMu.Lock()
	select {
	case p.queue <- task:
		recordExecPoolDepth(len(p.queue))
		// 入队成功后确保存在消费者：若所有 worker 都已释放令牌退出，
		// 此处必然能抢到令牌，启动一个只 drain 队列的 worker。
		select {
		case p.sem <- struct{}{}:
			p.exitMu.Unlock()
			p.inflight.Add(1)
			p.gate.RUnlock()
			go p.runWorker(nil)
		default:
			// 仍有 worker 持有令牌：其退出前的最终检查（exitMu 内）会消费该任务
			p.exitMu.Unlock()
			p.gate.RUnlock()
		}
		return true
	default:
		p.exitMu.Unlock()
		p.gate.RUnlock()
		return false
	}
}

// runWorker 是池 worker 的主循环：执行首个任务（可为 nil），随后持续
// drain 队列，直到在 exitMu 保护下确认队列为空才归还令牌退出。
func (p *ExecPool) runWorker(first func()) {
	defer p.inflight.Done()

	if first != nil {
		runPoolTask(first)
	}

	for {
		// 快路径批量消费，不持锁
		p.drainQueue()

		// 退出协议：在 exitMu 内做最终检查并归还令牌，
		// 与 TrySubmit 的「入队 + 确保消费者」动作互斥
		p.exitMu.Lock()
		select {
		case t := <-p.queue:
			recordExecPoolDepth(len(p.queue))
			p.exitMu.Unlock()
			runPoolTask(t)
		default:
			<-p.sem // 释放令牌（临界区内，令牌状态对 TrySubmit 可见）
			p.exitMu.Unlock()
			return
		}
	}
}

// drainQueue 从队列中消费任务，直到队列为空。
//
// 由已完成直接任务的 goroutine 调用，确保队列任务不会停滞。
// 由于队列消费时持有令牌，其他 goroutine 的 drainQueue 不会重复消费。
func (p *ExecPool) drainQueue() {
	p.drainQueueUntil(context.Background())
}

// drainQueueUntil 从队列中消费任务，直到队列为空或 ctx 结束。
//
// 每取一个任务前检查 ctx：一旦取消就不再启动新任务（已开始的任务
// 无法被 Go 中断，但不再继续消费队列）。返回 false 表示 ctx 先结束。
func (p *ExecPool) drainQueueUntil(ctx context.Context) bool {
	for {
		if ctx.Err() != nil {
			return false
		}
		select {
		case task := <-p.queue:
			recordExecPoolDepth(len(p.queue))
			runPoolTask(task)
		default:
			return true
		}
	}
}

// runPoolTask 执行池任务并兜底 recover。
//
// 池中的任务运行在独立 goroutine 里，任何逃逸出来的 panic 都会直接终止进程。
// invokeHandler 内部的 recover 只覆盖 handler 本身，中间件链的构造过程
// （chain[i](tmp[i+1])）不在其保护范围内；同步路径上还有 processEventGuard
// 兜底，池路径此前则完全没有保护。
func runPoolTask(task func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithFields(logger.Fields{
				"panic": r,
				"stack": string(debug.Stack()),
			}).Error("[engine] Panic recovered in ExecPool task")
		}
	}()
	task()
}

// Drain 等待所有正在执行的任务完成，直到超时或 context 取消。
//
// 应在 shutdown 时调用。调用后不应再提交新任务。
// 在等待执行中 goroutine 之前，会先消费队列中的剩余任务；ctx 结束时立即停止
// 消费并返回，不会为了清空队列而忽略取消（否则一次长队列/阻塞任务就能
// 让 Shutdown 超过调用方给定超时）。
func (p *ExecPool) Drain(ctx context.Context) error {
	p.stopped.Store(true)
	// 写锁屏障：等待所有已通过 stopped 检查的 TrySubmit 完成登记与入队，
	// 保证后续 inflight.Wait 能看到它们（否则任务可能在 Drain 返回后才跑）。
	p.gate.Lock()
	p.gate.Unlock() //nolint:staticcheck // SA2001 intentional drain barrier — 确保在途 TrySubmit 已全部登记

	// 先消费队列中可能残留的任务（当没有活跃 goroutine 来 drain 时）
	if !p.drainQueueUntil(ctx) {
		return ctx.Err()
	}

	return p.inflight.Wait(ctx)
}

// Stop 停止接受新任务并等待所有正在运行的任务完成（默认 30 秒超时）。
func (p *ExecPool) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = p.Drain(ctx)
}
