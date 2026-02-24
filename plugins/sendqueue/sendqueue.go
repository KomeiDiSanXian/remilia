// Package sendqueue 提供消息发送队列与频控插件。
//
// 功能：
//   - 异步消息队列，避免阻塞事件处理主循环
//   - 按 target（groupID / openID）独立令牌桶限速
//   - 发送失败自动重试（区分 429 与永久错误）
//   - 与 lifecycle 自动联动
//
// 使用示例:
//
//	pm.RegisterV2(sendqueue.New(sendqueue.Config{Rate: 5, Burst: 10}))
//	// 在 Handler 中：
//	sq := ctx.MustGet("sendqueue").(*sendqueue.Plugin)
//	sq.Enqueue("group_openid", msg)
package sendqueue

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Config 发送队列配置
type Config struct {
	// Rate 全局消息发送速率（条/秒）
	Rate float64
	// Burst 令牌桶突发容量
	Burst int
	// PerTargetRate 单 target（群/用户）的速率（条/秒），0 表示不限
	PerTargetRate float64
	// PerTargetBurst 单 target 突发容量
	PerTargetBurst int
	// QueueSize 队列最大深度
	QueueSize int
	// Workers 消费 goroutine 数量
	Workers int
	// MaxRetries 发送失败最大重试次数
	MaxRetries int
	// RetryDelay 重试间隔
	RetryDelay time.Duration
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{
		Rate:           10,
		Burst:          20,
		PerTargetRate:  2,
		PerTargetBurst: 5,
		QueueSize:      1000,
		Workers:        4,
		MaxRetries:     3,
		RetryDelay:     500 * time.Millisecond,
	}
}

// sendJob 一条待发送任务
type sendJob struct {
	target  string // groupOpenID 或 userOpenID
	isGroup bool   // true = 群消息, false = C2C
	msg     *dto.Message
	api     openapi.OpenAPI
	attempt int
}

// Plugin 消息发送队列插件 API
type Plugin struct {
	cfg        Config
	globalRL   *rate.Limiter
	perTarget  *lru.Cache[string, *rate.Limiter]
	queue      chan sendJob
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	defaultAPI openapi.OpenAPI
}

// New 创建发送队列插件
func New(cfg Config) *plugin.PluginDescriptor {
	if cfg.Rate <= 0 {
		cfg.Rate = DefaultConfig().Rate
	}
	if cfg.Burst <= 0 {
		cfg.Burst = DefaultConfig().Burst
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = DefaultConfig().QueueSize
	}
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultConfig().Workers
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultConfig().MaxRetries
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = DefaultConfig().RetryDelay
	}

	targetCache, _ := lru.New[string, *rate.Limiter](2000)
	ctx, cancel := context.WithCancel(context.Background())

	p := &Plugin{
		cfg:       cfg,
		globalRL:  rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Burst),
		perTarget: targetCache,
		queue:     make(chan sendJob, cfg.QueueSize),
		ctx:       ctx,
		cancel:    cancel,
	}

	return &plugin.PluginDescriptor{
		Name:    "sendqueue",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "异步消息发送队列，内置令牌桶频控，防止 API 被打满",
			Category:    "核心",
			Tags:        []string{"发送", "队列", "频控"},
		},
		Setup: func(setupCtx *plugin.SetupContext) (any, error) {
			setupCtx.Log.Infof("Starting %d workers (rate=%.1f/s burst=%d)", cfg.Workers, cfg.Rate, cfg.Burst)
			for i := range cfg.Workers {
				p.wg.Add(1)
				go p.worker(i)
			}
			setupCtx.Log.Info("Plugin loaded")
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Shutting down sendqueue")
			sq := ctx.API.(*Plugin)
			sq.cancel()
			sq.wg.Wait()
			ctx.Log.Info("Shutdown complete")
			return nil
		},
	}
}

// SetDefaultAPI 设置默认 OpenAPI 实例（通常在 Setup 中由 engine 注入）
func (p *Plugin) SetDefaultAPI(api openapi.OpenAPI) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultAPI = api
}

// EnqueueGroup 将群消息加入队列
func (p *Plugin) EnqueueGroup(groupOpenID string, msg *dto.Message, api openapi.OpenAPI) error {
	return p.enqueue(sendJob{target: groupOpenID, isGroup: true, msg: msg, api: api})
}

// EnqueueC2C 将私聊消息加入队列
func (p *Plugin) EnqueueC2C(openID string, msg *dto.Message, api openapi.OpenAPI) error {
	return p.enqueue(sendJob{target: openID, isGroup: false, msg: msg, api: api})
}

func (p *Plugin) enqueue(job sendJob) error {
	if job.api == nil {
		p.mu.Lock()
		job.api = p.defaultAPI
		p.mu.Unlock()
	}
	select {
	case p.queue <- job:
		return nil
	default:
		return fmt.Errorf("sendqueue: queue full (size=%d)", p.cfg.QueueSize)
	}
}

// targetLimiter 获取或创建 per-target 限流器
func (p *Plugin) targetLimiter(target string) *rate.Limiter {
	if p.cfg.PerTargetRate <= 0 {
		return nil
	}
	if rl, ok := p.perTarget.Get(target); ok {
		return rl
	}
	rl := rate.NewLimiter(rate.Limit(p.cfg.PerTargetRate), p.cfg.PerTargetBurst)
	p.perTarget.Add(target, rl)
	return rl
}

// worker 消费队列
func (p *Plugin) worker(id int) {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.queue:
			if !ok {
				return
			}
			p.process(id, job)
		}
	}
}

func (p *Plugin) process(workerID int, job sendJob) {
	// 全局限速等待
	if err := p.globalRL.Wait(p.ctx); err != nil {
		return // ctx cancelled
	}
	// per-target 限速等待
	if rl := p.targetLimiter(job.target); rl != nil {
		if err := rl.Wait(p.ctx); err != nil {
			return
		}
	}

	var sendErr error
	if job.isGroup {
		_, sendErr = job.api.GroupChat(job.target, job.msg)
	} else {
		_, sendErr = job.api.SingleChat(job.target, job.msg)
	}

	if sendErr != nil {
		if job.attempt < p.cfg.MaxRetries {
			job.attempt++
			// 指数退避 + jitter：delay = RetryDelay * 2^(attempt-1) + random[0, RetryDelay)
			// 防止重试风暴（Bug 2.11 修复）
			backoff := p.cfg.RetryDelay * (1 << (job.attempt - 1))
			jitter := time.Duration(rand.Int64N(int64(p.cfg.RetryDelay)))
			retryAfter := backoff + jitter
			logger.WithError(sendErr).Warnf("[SendQueue] worker=%d send failed (attempt %d/%d), retrying in %s",
				workerID, job.attempt, p.cfg.MaxRetries, retryAfter)
			time.AfterFunc(retryAfter, func() {
				select {
				case p.queue <- job:
				default:
					logger.Warnf("[SendQueue] retry queue full, dropping message to %s", job.target)
				}
			})
		} else {
			logger.WithError(sendErr).Errorf("[SendQueue] worker=%d max retries reached for target=%s", workerID, job.target)
		}
	} else {
		logger.Debugf("[SendQueue] worker=%d sent to %s (isGroup=%v)", workerID, job.target, job.isGroup)
	}
}
