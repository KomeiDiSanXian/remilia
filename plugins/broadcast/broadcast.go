// Package broadcast 提供广播/推送插件。
//
// 功能：
//   - 向多个群或用户批量发送消息
//   - 内置发送速率控制（复用 sendqueue 或直接内置令牌桶）
//   - 发送结果统计（成功/失败数）
//   - 依赖 openapi.OpenAPI 进行实际发送
//
// 使用示例:
//
//	pm.RegisterV2(broadcast.New())
//	bc := ctx.MustGet("broadcast").(*broadcast.Plugin)
//	bc.SetAPI(ctx.GetAPI())
//	result := bc.ToGroups([]string{"group1", "group2"}, dto.TextMsg("公告内容"))
package broadcast

import (
	"context"
	"sync"
	"sync/atomic"

	"golang.org/x/time/rate"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Result 广播发送结果
type Result struct {
	Total   int
	Success int
	Failed  int
	Errors  []error
}

// Config 广播插件配置
type Config struct {
	// Rate 全局发送速率（条/秒）
	Rate float64
	// Burst 令牌桶突发容量
	Burst int
	// Concurrency 并发发送数
	Concurrency int
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{Rate: 5, Burst: 10, Concurrency: 4}
}

// Plugin 广播插件 API
type Plugin struct {
	cfg Config
	rl  *rate.Limiter
	api openapi.OpenAPI
	mu  sync.RWMutex
}

// New 创建广播插件描述符
func New(cfg ...Config) *plugin.PluginDescriptor {
	c := DefaultConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	p := &Plugin{
		cfg: c,
		rl:  rate.NewLimiter(rate.Limit(c.Rate), c.Burst),
	}

	return &plugin.PluginDescriptor{
		Name:        "broadcast",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "广播/推送插件，支持向多群和多用户批量发送消息",
		Category:    "核心",
		Tags:        []string{"广播", "推送", "通知"},
		Deps:        []string{},
		HelpText: `广播插件使用说明：
  bc := ctx.MustGet("broadcast").(*broadcast.Plugin)
  bc.SetAPI(api)
  result := bc.ToGroups(groupIDs, msg)
  result := bc.ToC2C(openIDs, msg)`,

		Setup: func(setupCtx *plugin.SetupContext) error {
			logger.Infof("[Broadcast] Plugin loaded (rate=%.1f/s concurrency=%d)", c.Rate, c.Concurrency)
			setupCtx.Manager.GetContainer().Register("broadcast", p)
			return nil
		},
	}
}

// SetAPI 设置 OpenAPI 实例（必须在使用前调用）
func (p *Plugin) SetAPI(api openapi.OpenAPI) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.api = api
}

// getAPI 线程安全地获取 API
func (p *Plugin) getAPI() openapi.OpenAPI {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.api
}

// ToGroups 向多个群发送消息，返回汇总结果
func (p *Plugin) ToGroups(groupIDs []string, msg *dto.Message) Result {
	return p.send(groupIDs, msg, true)
}

// ToC2C 向多个用户发送私聊消息，返回汇总结果
func (p *Plugin) ToC2C(openIDs []string, msg *dto.Message) Result {
	return p.send(openIDs, msg, false)
}

// ToAll 向 groups 和 c2c 用户同时广播
func (p *Plugin) ToAll(groupIDs, openIDs []string, msg *dto.Message) (groupResult, c2cResult Result) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); groupResult = p.ToGroups(groupIDs, msg) }()
	go func() { defer wg.Done(); c2cResult = p.ToC2C(openIDs, msg) }()
	wg.Wait()
	return
}

func (p *Plugin) send(targets []string, msg *dto.Message, isGroup bool) Result {
	api := p.getAPI()
	if api == nil {
		logger.Error("[Broadcast] OpenAPI not set, call SetAPI() first")
		return Result{Total: len(targets), Failed: len(targets)}
	}

	result := Result{Total: len(targets)}
	var (
		mu      sync.Mutex
		success int64
		failed  int64
		errs    []error
		wg      sync.WaitGroup
		sem     = make(chan struct{}, p.cfg.Concurrency)
	)

	ctx := context.Background()
	for _, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(t string) {
			defer wg.Done()
			defer func() { <-sem }()

			// 频控等待
			_ = p.rl.Wait(ctx)

			var sendErr error
			if isGroup {
				_, sendErr = api.GroupChat(t, msg)
			} else {
				_, sendErr = api.SingleChat(t, msg)
			}

			if sendErr != nil {
				atomic.AddInt64(&failed, 1)
				mu.Lock()
				errs = append(errs, sendErr)
				mu.Unlock()
				logger.WithError(sendErr).Warnf("[Broadcast] Failed to send to %s", t)
			} else {
				atomic.AddInt64(&success, 1)
				logger.Debugf("[Broadcast] Sent to %s", t)
			}
		}(target)
	}
	wg.Wait()

	result.Success = int(success)
	result.Failed = int(failed)
	result.Errors = errs

	logger.Infof("[Broadcast] Completed: total=%d success=%d failed=%d", result.Total, result.Success, result.Failed)
	return result
}
