package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
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
	maxConcurrency int
	queueSize      int

	sem     chan struct{} // 并发令牌，容量 = maxConcurrency
	queue   chan func()   // 有界等待队列，容量 = queueSize
	stopped atomic.Bool
	wg      sync.WaitGroup
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
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 64
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 128
	}
	return &ExecPool{
		maxConcurrency: cfg.MaxConcurrency,
		queueSize:      cfg.QueueSize,
		sem:            make(chan struct{}, cfg.MaxConcurrency),
		queue:          make(chan func(), cfg.QueueSize),
	}
}

// Wait 等待所有已提交的任务完成（不停止池，测试用）。
//
// 仅在测试中需要确保异步 handler 完成时使用。
func (p *ExecPool) Wait() {
	p.wg.Wait()
}

// TrySubmit 尝试提交任务到池中执行。
//
//   - 如果池中有空闲 slot，立即启动 goroutine 执行。
//   - 如果池满但有队列空间，任务排队等待。
//   - 如果池满且队列满，返回 false（调用方应 fallback 同步执行）。
func (p *ExecPool) TrySubmit(task func()) bool {
	if p.stopped.Load() {
		return false
	}

	// 尝试获取令牌（非阻塞）
	select {
	case p.sem <- struct{}{}:
		// 获取到令牌，启动 goroutine
		p.wg.Add(1)
		go func() {
			defer func() {
				<-p.sem // 释放令牌
				p.wg.Done()
			}()
			task()
		}()
		return true
	default:
		// 池满，尝试入队
	}

	// 尝试入队（非阻塞）
	select {
	case p.queue <- task:
		if !p.stopped.Load() {
			select {
			case p.sem <- struct{}{}:
				p.wg.Add(1)
				go func() {
					defer func() {
						<-p.sem
						p.wg.Done()
					}()
					p.drainQueue()
				}()
			default:
			}
		}
		return true
	default:
		return false
	}
}

// drainQueue 从队列中消费任务，直到队列为空或池停止。
func (p *ExecPool) drainQueue() {
	for {
		select {
		case task := <-p.queue:
			task()
		default:
			return
		}
	}
}

// Drain 等待所有正在执行的任务完成，直到超时或 context 取消。
//
// 应在 shutdown 时调用。调用后不应再提交新任务。
func (p *ExecPool) Drain(ctx context.Context) error {
	p.stopped.Store(true)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop 停止接受新任务并等待所有正在运行的任务完成（默认 30 秒超时）。
func (p *ExecPool) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = p.Drain(ctx)
}
