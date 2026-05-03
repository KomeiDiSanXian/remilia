package job

import (
	stdctx "context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 结构化后台作业系统 API。
// 通过 plugin.Must[*job.Plugin](ctx, "job") 获取。
type Plugin struct {
	mu   sync.RWMutex
	jobs map[ID]*entry

	// lifecycleCtx 绑定 Bot 生命周期；Bot 停止时所有 pending 作业收到取消信号。
	lifecycleCtx    stdctx.Context
	lifecycleCancel stdctx.CancelFunc
}

// entry 内部作业运行记录。
type entry struct {
	info   Info
	mu     sync.Mutex
	done   chan struct{} // 关闭时表示作业已终态（Done/Failed/Canceled）
	cancel stdctx.CancelFunc
}

// ── 构造 ────────────────────────────────────────────────────────────────────

// NewPlugin 创建 Plugin 实例（不注册到插件系统，供测试使用）。
func NewPlugin() *Plugin {
	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	return &Plugin{
		jobs:            make(map[ID]*entry),
		lifecycleCtx:    ctx,
		lifecycleCancel: cancel,
	}
}

func newEntry(id ID, name string) *entry {
	return &entry{
		info: Info{
			ID:          id,
			Name:        name,
			Status:      StatusPending,
			SubmittedAt: time.Now(),
		},
		done: make(chan struct{}),
	}
}

func (p *Plugin) register(e *entry) {
	p.mu.Lock()
	p.jobs[e.info.ID] = e
	p.mu.Unlock()
}

func (p *Plugin) getEntry(id ID) (*entry, bool) {
	p.mu.RLock()
	e, ok := p.jobs[id]
	p.mu.RUnlock()
	return e, ok
}

// ── 公共 API ─────────────────────────────────────────────────────────────────

// Once 提交一次性作业，可选延迟。
//
// 返回作业 ID，可用于 [Wait]、[Cancel]、[Info] 等操作。
func (p *Plugin) Once(name string, fn Func, opts ...Option) ID {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	id := ID(uuid.NewString())
	e := newEntry(id, name)
	p.register(e)
	go p.runOnce(e, fn, cfg)
	return id
}

// Retry 提交带自动重试的作业。
//
// 等效于 Once + WithMaxRetries(n) + WithBackoff(strategy)。
func (p *Plugin) Retry(name string, fn Func, opts ...Option) ID {
	return p.Once(name, fn, opts...)
}

// Chain 提交顺序链：fns 依次执行，任意步骤返回非 nil error 则整链失败。
//
// 整条链作为一个作业追踪，对外呈现单一 JobID。
func (p *Plugin) Chain(name string, fns ...Func) ID {
	return p.Once(name, func(ctx stdctx.Context) error {
		for i, fn := range fns {
			if err := fn(ctx); err != nil {
				return fmt.Errorf("chain step %d/%d: %w", i+1, len(fns), err)
			}
		}
		return nil
	})
}

// Cancel 取消仍在 Pending 状态的作业。
//
// 若作业已开始执行（StatusRunning），调用会触发 ctx 取消信号，
// 但是否真正中止取决于 fn 是否正确响应 ctx.Done()。
// 返回 true 表示取消信号已发送，false 表示作业 ID 不存在或已终态。
func (p *Plugin) Cancel(id ID) bool {
	e, ok := p.getEntry(id)
	if !ok {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.info.Status == StatusDone || e.info.Status == StatusFailed || e.info.Status == StatusCanceled {
		return false
	}
	if e.cancel != nil {
		e.cancel()
	}
	return true
}

// Info 返回指定作业的状态快照（不可变副本）。
// 第二个返回值为 false 表示作业 ID 不存在。
func (p *Plugin) Info(id ID) (Info, bool) {
	e, ok := p.getEntry(id)
	if !ok {
		return Info{}, false
	}
	e.mu.Lock()
	info := e.info
	e.mu.Unlock()
	return info, true
}

// Wait 阻塞直到指定作业达到终态（Done/Failed/Canceled），或 ctx 被取消。
//
// 返回值：
//   - nil：作业成功完成（StatusDone）
//   - info.LastError：作业失败时的最终错误
//   - ctx.Err()：等待超时或 ctx 被取消
//   - ErrJobNotFound：作业 ID 不存在
func (p *Plugin) Wait(ctx stdctx.Context, id ID) error {
	e, ok := p.getEntry(id)
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	select {
	case <-e.done:
		e.mu.Lock()
		info := e.info
		e.mu.Unlock()
		if info.Status == StatusFailed {
			return info.LastError
		}
		if info.Status == StatusCanceled {
			return fmt.Errorf("job %q canceled", id)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ── 内部执行 ──────────────────────────────────────────────────────────────────

func (p *Plugin) runOnce(e *entry, fn Func, cfg jobConfig) {
	// 延迟等待
	if cfg.delay > 0 {
		t := time.NewTimer(cfg.delay)
		select {
		case <-t.C:
		case <-p.lifecycleCtx.Done():
			t.Stop()
			p.finalize(e, StatusCanceled, nil, cfg)
			return
		}
	}

	// 执行（含重试）
	maxAttempts := 1 + cfg.maxRetries
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// 检查是否已被取消
		select {
		case <-p.lifecycleCtx.Done():
			p.finalize(e, StatusCanceled, nil, cfg)
			return
		default:
		}

		// 构建本次执行的 ctx
		runCtx, runCancel := stdctx.WithCancel(p.lifecycleCtx)
		if cfg.timeout > 0 {
			runCtx, runCancel = stdctx.WithTimeout(p.lifecycleCtx, cfg.timeout)
		}
		// 注册 cancel 函数，供 Cancel() 使用
		e.mu.Lock()
		e.cancel = runCancel
		e.info.Status = StatusRunning
		e.info.Attempts = attempt
		if attempt == 1 {
			e.info.StartedAt = time.Now()
		}
		e.mu.Unlock()

		err := fn(runCtx)
		runCancel()

		if err == nil {
			p.finalize(e, StatusDone, nil, cfg)
			return
		}

		// 记录本次错误
		e.mu.Lock()
		e.info.LastError = err
		e.mu.Unlock()

		logger.Warnf("[job] %q attempt %d/%d failed: %v", e.info.Name, attempt, maxAttempts, err)

		// 若还有重试机会，则退避等待
		if attempt < maxAttempts {
			waitD := time.Duration(0)
			if cfg.backoff != nil {
				waitD = cfg.backoff(attempt)
			}
			if waitD > 0 {
				t := time.NewTimer(waitD)
				select {
				case <-t.C:
				case <-p.lifecycleCtx.Done():
					t.Stop()
					p.finalize(e, StatusCanceled, nil, cfg)
					return
				}
			}
		}
	}

	// 所有尝试均失败
	e.mu.Lock()
	finalErr := e.info.LastError
	e.mu.Unlock()
	p.finalize(e, StatusFailed, finalErr, cfg)
}

func (p *Plugin) finalize(e *entry, status Status, err error, cfg jobConfig) {
	e.mu.Lock()
	e.info.Status = status
	e.info.FinishedAt = time.Now()
	if err != nil {
		e.info.LastError = err
	}
	info := e.info
	e.mu.Unlock()

	close(e.done) // 通知 Wait 等待者

	if cfg.onDone != nil {
		cfg.onDone(info)
	}

	switch status {
	case StatusDone:
		logger.Infof("[job] %q (%s) done after %d attempt(s)",
			info.Name, info.ID, info.Attempts)
	case StatusFailed:
		logger.Errorf("[job] %q (%s) failed after %d attempt(s): %v",
			info.Name, info.ID, info.Attempts, err)
	case StatusCanceled:
		logger.Infof("[job] %q (%s) canceled", info.Name, info.ID)
	}
}

// ── 插件描述符 ─────────────────────────────────────────────────────────────

// New 创建作业系统插件描述符并注册到 PluginManager。
//
//	pm.Register(job.New())
//	// 其他插件中：
//	runner := plugin.Must[*job.Plugin](ctx, "job")
//	runner.Once("my-task", fn, job.WithDelay(5*time.Second))
func New() *plugin.Descriptor {
	p := NewPlugin()
	return &plugin.Descriptor{
		Name:    "job",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "结构化后台作业系统：一次性延迟任务、自动重试（指数退避）、顺序链，与 Bot 生命周期绑定",
			Category:    "基础设施",
			Tags:        []string{"任务", "作业", "重试", "延迟", "后台"},
			HelpText:    "job 插件提供 Once/Retry/Chain 三种后台作业模式，区别于 scheduler 的周期性 cron 任务。",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if !ctx.DryRun {
				// 绑定 Bot 生命周期：Bot 停止时取消所有 pending 作业
				ctx.Go(func(lifectx stdctx.Context) {
					<-lifectx.Done()
					p.lifecycleCancel()
				})
			}
			return p, nil
		},
	}
}
