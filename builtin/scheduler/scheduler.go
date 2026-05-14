// Package scheduler 提供计划任务插件。
//
// 支持两种调度方式：
//   - Every(d, fn)  — 固定间隔重复执行
//   - Cron(expr, fn) — cron 表达式（5 段标准格式：分 时 日 月 周）
//
// 任务与 Bot 生命周期自动绑定，无需手动启停。
//
// 使用示例:
//
//	pm.Register(scheduler.New())
//	// 在其他插件 Setup 中：
//	schedSvc := plugin.Service[*scheduler.Plugin](ctx, "scheduler")
//	sched.Every(5*time.Minute, func() { /* cleanup */ })
//	sched.Cron("0 9 * * *", func() { /* daily report */ })
package scheduler

import (
	stdctx "context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// JobFunc 任务函数签名
type JobFunc func()

// JobID 任务唯一标识
type JobID int

// JobRecord 任务执行记录
type JobRecord struct {
	JobID    JobID
	JobName  string
	StartAt  time.Time
	Duration time.Duration
	Success  bool
	Error    string
}

// jobEntry 内部任务记录
type jobEntry struct {
	id      JobID
	name    string
	cronID  cron.EntryID      // robfig/cron 条目 ID（仅用于 cron 调度）
	cancel  stdctx.CancelFunc // ticker 任务的独立取消函数（派生自 lifecycleCtx）
	running atomic.Bool       // 防重入标志
}

// historySize 保存的最大历史记录数（环形缓冲）
const historySize = 100

// Plugin 计划任务插件 API
type Plugin struct {
	c      *cron.Cron
	mu     sync.Mutex
	jobs   map[JobID]*jobEntry
	nextID JobID

	// lifecycleCtx 由框架 goroutineManager 驱动，用于统一停止所有 ticker goroutine
	lifecycleCtx    stdctx.Context
	lifecycleCancel stdctx.CancelFunc

	// 任务执行历史（环形缓冲）
	historyMu sync.RWMutex
	history   []JobRecord
}

// NewPlugin 创建并返回一个 Scheduler Plugin 实例。
// 配合 p.Descriptor() 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := scheduler.NewPlugin()
//	pm.Register(p.Descriptor())
//	p.Every(time.Minute, fn) // 直接调用
func NewPlugin() *Plugin {
	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	return &Plugin{
		jobs:            make(map[JobID]*jobEntry),
		lifecycleCtx:    ctx,
		lifecycleCancel: cancel,
	}
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.Register 使用。
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "scheduler",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "计划任务插件，支持固定间隔和 cron 表达式两种调度方式",
			Category:    "核心",
			Tags:        []string{"定时", "调度", "cron"},
			HelpText: `计划任务插件使用说明：
  p := scheduler.NewPlugin()
  pm.Register(p.Descriptor())
  p.Every(5*time.Minute, func() { ... })
  p.Cron("0 9 * * *", func() { ... })`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Loading scheduler plugin")
			p.c = cron.New(cron.WithSeconds())
			p.c.Start()
			// 将框架生命周期 context 注入 Plugin，供后续 Every() 派生 job context 使用。
			// 通过 ready channel 保证 Setup 返回前 lifecycleCtx 已切换完毕，
			// 使调用者拿到已注册 Plugin 后调用 Every() 时使用的是框架 runCtx
			ready := make(chan struct{})
			ctx.Go(func(runCtx stdctx.Context) {
				p.mu.Lock()
				// 直接替换为 runCtx 的子 context，不取消旧 background context
				// 旧 context 会在 NewPlugin 的 cancel 被调用时才结束（或随 GC 回收）
				p.lifecycleCtx, p.lifecycleCancel = stdctx.WithCancel(runCtx)
				p.mu.Unlock()
				close(ready)
				<-runCtx.Done()
				// runCtx 结束时，lifecycleCtx（其子 context）也随之取消
			})
			<-ready // 等待 goroutine 完成 lifecycleCtx 切换后再返回
			ctx.Log.Info("Scheduler plugin loaded")
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Stopping scheduler")
			sched := ctx.API.(*Plugin)
			// ticker goroutine 通过框架 goroutineManager 通过 lifecycleCtx 取消
			// 此处只需停止 robfig/cron（负责 cron 表达式任务）
			if sched.c != nil {
				stopCtx := sched.c.Stop()
				<-stopCtx.Done()
			}
			ctx.Log.Info("Scheduler stopped")
			return nil
		},
	}
}

// New 创建计划任务插件描述符（便捷入口，内部创建 Plugin 实例）。
// 若需要持有 Plugin 引用，改用 NewPlugin() + p.Descriptor()。
func New() *plugin.Descriptor {
	return NewPlugin().Descriptor()
}

// Get 从插件管理器中获取已注册的 Scheduler 插件实例（类型安全）。
// 需在 pm.Register(New()) 之后调用。
func Get(pm *plugin.Manager) *Plugin {
	v, ok := pm.GetContainer().Get("scheduler")
	if !ok {
		panic("scheduler: plugin not registered; call pm.Register(scheduler.New()) first")
	}
	p, ok := v.(*Plugin)
	if !ok {
		panic("scheduler: unexpected type in container")
	}
	return p
}

// Every 注册固定间隔任务，返回可用于 Remove 的 JobID。
// fn 是幂等保护的：若上一次执行尚未完成，本次调度跳过。
func (p *Plugin) Every(d time.Duration, fn JobFunc) JobID {
	return p.every(fmt.Sprintf("every(%s)", d), d, fn)
}

// EveryNamed 注册命名的固定间隔任务
func (p *Plugin) EveryNamed(name string, d time.Duration, fn JobFunc) JobID {
	return p.every(name, d, fn)
}

func (p *Plugin) every(name string, d time.Duration, fn JobFunc) JobID {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.nextID++
	id := p.nextID
	entry := &jobEntry{id: id, name: name}

	if d < time.Second {
		// 固定间隔 < 1s：使用 goroutine + ticker
		// 每个 job 有独立 context（派生自 lifecycleCtx），支持单独 Remove 和整体停止
		jobCtx, jobCancel := stdctx.WithCancel(p.lifecycleCtx)
		entry.cancel = jobCancel
		go func() {
			defer jobCancel() // 确保 context 资源被释放
			ticker := time.NewTicker(d)
			defer ticker.Stop()
			wrapped := p.wrap(entry, fn)
			for {
				select {
				case <-ticker.C:
					wrapped()
				case <-jobCtx.Done():
					return
				}
			}
		}()
	} else {
		// >= 1s：交给 robfig/cron 管理，cancel 为 nil
		cronID := p.c.Schedule(cron.Every(d), cron.FuncJob(p.wrap(entry, fn)))
		entry.cronID = cronID
	}

	p.jobs[id] = entry
	logger.Infof("[Scheduler] Registered job '%s' every %s (id=%d)", name, d, id)
	return id
}

// Cron 注册 cron 表达式任务（支持 5 段标准格式：分 时 日 月 周，或 6 段含秒格式）
func (p *Plugin) Cron(expr string, fn JobFunc) JobID {
	return p.cronNamed(expr, expr, fn)
}

// CronNamed 注册命名的 cron 任务
func (p *Plugin) CronNamed(name, expr string, fn JobFunc) JobID {
	return p.cronNamed(name, expr, fn)
}

func (p *Plugin) cronNamed(name, expr string, fn JobFunc) JobID {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.nextID++
	id := p.nextID
	entry := &jobEntry{id: id, name: name}

	cronID, err := p.c.AddFunc(expr, p.wrap(entry, fn))
	if err != nil {
		logger.WithError(err).Errorf("[Scheduler] Failed to add cron job '%s' expr=%s", name, expr)
		return 0
	}
	entry.cronID = cronID
	p.jobs[id] = entry
	logger.Infof("[Scheduler] Registered cron job '%s' expr=%q (id=%d)", name, expr, id)
	return id
}

// Remove 移除任务
func (p *Plugin) Remove(id JobID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.jobs[id]; ok {
		if entry.cancel != nil {
			// ticker 任务：取消其独立 context，goroutine 随即退出
			entry.cancel()
		} else if entry.cronID != 0 {
			// cron 任务：从 cron scheduler 移除
			p.c.Remove(entry.cronID)
		}
		delete(p.jobs, id)
		logger.Infof("[Scheduler] Removed job id=%d name=%s", id, entry.name)
	}
}

// Jobs 返回当前已注册的任务数量
func (p *Plugin) Jobs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.jobs)
}

// wrap 包装任务函数，加入防重入保护、panic 恢复和执行历史记录
func (p *Plugin) wrap(entry *jobEntry, fn JobFunc) func() {
	return func() {
		if !entry.running.CompareAndSwap(false, true) {
			logger.Warnf("[Scheduler] Job '%s' (id=%d) is still running, skipping", entry.name, entry.id)
			return
		}
		defer entry.running.Store(false)

		start := time.Now()
		rec := JobRecord{
			JobID:   entry.id,
			JobName: entry.name,
			StartAt: start,
			Success: true,
		}

		defer func() {
			rec.Duration = time.Since(start)
			if r := recover(); r != nil {
				logger.Errorf("[Scheduler] Panic in job '%s' (id=%d): %v", entry.name, entry.id, r)
				rec.Success = false
				rec.Error = fmt.Sprintf("panic: %v", r)
			}
			logger.Debugf("[Scheduler] Job '%s' (id=%d) completed in %s success=%v",
				entry.name, entry.id, rec.Duration, rec.Success)
			p.appendHistory(rec)
		}()
		fn()
	}
}

// appendHistory 追加执行记录到环形缓冲
func (p *Plugin) appendHistory(rec JobRecord) {
	p.historyMu.Lock()
	defer p.historyMu.Unlock()
	if len(p.history) >= historySize {
		// 丢弃最旧的记录
		p.history = p.history[1:]
	}
	p.history = append(p.history, rec)
}

// History 返回最近任务执行记录（最多 n 条，n<=0 时返回全部）
func (p *Plugin) History(n int) []JobRecord {
	p.historyMu.RLock()
	defer p.historyMu.RUnlock()
	if n <= 0 || n >= len(p.history) {
		out := make([]JobRecord, len(p.history))
		copy(out, p.history)
		return out
	}
	// 返回最近 n 条（末尾）
	out := make([]JobRecord, n)
	copy(out, p.history[len(p.history)-n:])
	return out
}
