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
//	pm.RegisterV2(scheduler.New())
//	// 在其他插件 Setup 中：
//	sched := ctx.MustGet("scheduler").(*scheduler.Plugin)
//	sched.Every(5*time.Minute, func() { /* cleanup */ })
//	sched.Cron("0 9 * * *", func() { /* daily report */ })
package scheduler

import (
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
	cronID  cron.EntryID  // robfig/cron 条目 ID（仅用于 cron 调度）
	stopCh  chan struct{} // 用于 ticker 调度的停止信号
	running atomic.Bool   // 防重入标志
}

// historySize 保存的最大历史记录数（环形缓冲）
const historySize = 100

// Plugin 计划任务插件 API
type Plugin struct {
	c      *cron.Cron
	mu     sync.Mutex
	jobs   map[JobID]*jobEntry
	nextID JobID

	// 任务执行历史（环形缓冲）
	historyMu sync.RWMutex
	history   []JobRecord
}

// NewPlugin 创建并返回一个 Scheduler Plugin 实例。
// 配合 Descriptor(p) 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := scheduler.NewPlugin()
//	pm.RegisterV2(scheduler.Descriptor(p))
//	p.Every(time.Minute, fn) // 直接调用
func NewPlugin() *Plugin {
	return &Plugin{jobs: make(map[JobID]*jobEntry)}
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.RegisterV2 使用。
func Descriptor(p *Plugin) *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:    "scheduler",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "计划任务插件，支持固定间隔和 cron 表达式两种调度方式",
			Category:    "核心",
			Tags:        []string{"定时", "调度", "cron"},
			HelpText: `计划任务插件使用说明：
  p := scheduler.NewPlugin()
  pm.RegisterV2(scheduler.Descriptor(p))
  p.Every(5*time.Minute, func() { ... })
  p.Cron("0 9 * * *", func() { ... })`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Loading scheduler plugin")
			p.c = cron.New(cron.WithSeconds())
			p.c.Start()
			ctx.Log.Info("Scheduler plugin loaded")
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Stopping scheduler")
			sched := ctx.API.(*Plugin)
			sched.mu.Lock()
			for _, entry := range sched.jobs {
				if entry.stopCh != nil {
					close(entry.stopCh)
				}
			}
			sched.mu.Unlock()
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
// 若需要持有 Plugin 引用，改用 NewPlugin() + Descriptor()。
func New() *plugin.PluginDescriptor {
	return Descriptor(NewPlugin())
}

// Get 从插件管理器中获取已注册的 Scheduler 插件实例（类型安全）。
// 需在 pm.RegisterV2(New()) 之后调用。
func Get(pm *plugin.Manager) *Plugin {
	v, ok := pm.GetContainer().Get("scheduler")
	if !ok {
		panic("scheduler: plugin not registered; call pm.RegisterV2(scheduler.New()) first")
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
		// For sub-second intervals, use a ticker goroutine
		// since robfig/cron rounds up to 1 second minimum.
		entry.stopCh = make(chan struct{})
		go func() {
			ticker := time.NewTicker(d)
			defer ticker.Stop()
			wrapped := p.wrap(entry, fn)
			for {
				select {
				case <-ticker.C:
					wrapped()
				case <-entry.stopCh:
					return
				}
			}
		}()
	} else {
		// Use cron.Schedule for >= 1s intervals
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
		if entry.stopCh != nil {
			// Ticker-based job: signal goroutine to stop
			close(entry.stopCh)
		} else {
			// Cron-based job
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
