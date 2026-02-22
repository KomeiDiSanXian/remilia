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

// jobEntry 内部任务记录
type jobEntry struct {
	id      JobID
	name    string
	cronID  cron.EntryID  // robfig/cron 条目 ID（仅用于 cron 调度）
	stopCh  chan struct{} // 用于 ticker 调度的停止信号
	running atomic.Bool   // 防重入标志
}

// Plugin 计划任务插件 API
type Plugin struct {
	c      *cron.Cron
	mu     sync.Mutex
	jobs   map[JobID]*jobEntry
	nextID JobID
}

// New creates the scheduler plugin descriptor.
// Use NewPlugin() if you need a direct reference to the Plugin API.
func New() *plugin.PluginDescriptor {
	_, desc := NewPlugin()
	return desc
}

// NewPlugin creates the scheduler plugin and returns both the Plugin API and its descriptor.
// This is the preferred constructor when you need to call scheduler methods directly (e.g. in tests).
func NewPlugin() (*Plugin, *plugin.PluginDescriptor) {
	p := &Plugin{
		jobs: make(map[JobID]*jobEntry),
	}

	desc := &plugin.PluginDescriptor{
		Name:        "scheduler",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "计划任务插件，支持固定间隔和 cron 表达式两种调度方式",
		Category:    "核心",
		Tags:        []string{"定时", "调度", "cron"},
		Deps:        []string{},
		HelpText: `计划任务插件使用说明：
  sched := ctx.MustGet("scheduler").(*scheduler.Plugin)
  sched.Every(5*time.Minute, func() { ... })        // 固定间隔
  sched.Cron("0 9 * * *", func() { ... })           // cron 表达式
  sched.Remove(id)                                  // 移除任务`,

		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[Scheduler] Loading scheduler plugin...")
			p.c = cron.New(cron.WithSeconds())
			p.c.Start()
			ctx.Manager.GetContainer().Register("scheduler", p)
			logger.Info("[Scheduler] Scheduler plugin loaded")
			return nil
		},

		Teardown: func() error {
			logger.Info("[Scheduler] Stopping scheduler...")
			// Stop ticker-based goroutines
			p.mu.Lock()
			for _, entry := range p.jobs {
				if entry.stopCh != nil {
					close(entry.stopCh)
				}
			}
			p.mu.Unlock()
			// Stop cron scheduler
			if p.c != nil {
				stopCtx := p.c.Stop()
				<-stopCtx.Done()
			}
			logger.Info("[Scheduler] Scheduler stopped")
			return nil
		},
	}
	return p, desc
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

// wrap 包装任务函数，加入防重入保护和 panic 恢复
func (p *Plugin) wrap(entry *jobEntry, fn JobFunc) func() {
	return func() {
		if !entry.running.CompareAndSwap(false, true) {
			logger.Warnf("[Scheduler] Job '%s' (id=%d) is still running, skipping", entry.name, entry.id)
			return
		}
		defer entry.running.Store(false)
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[Scheduler] Panic in job '%s' (id=%d): %v", entry.name, entry.id, r)
			}
		}()
		start := time.Now()
		fn()
		logger.Debugf("[Scheduler] Job '%s' (id=%d) completed in %s", entry.name, entry.id, time.Since(start))
	}
}
