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
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ErrAPINotSet 未调用 SetAPI 时发送会返回此错误
var ErrAPINotSet = fmt.Errorf("broadcast: OpenAPI not set, call SetAPI() first")

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

	// 订阅管理
	groupSubs map[string]bool // groupOpenID -> subscribed
	c2cSubs   map[string]bool // userOpenID -> subscribed
	subMu     sync.RWMutex

	// 可选持久化后端
	storage storageBackend
}

// storageBackend 用于订阅持久化（避免直接依赖 storage 包）
type storageBackend interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, ttl time.Duration) error
}

// NewPlugin 创建 Plugin 实例（用于测试或需要持有引用的场景）
// 配合 Descriptor(p) 使用，或直接调用 p.SetAPI(api) / p.ToGroups(...)。
func NewPlugin(cfg Config) *Plugin {
	return &Plugin{
		cfg:       cfg,
		rl:        rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Burst),
		groupSubs: make(map[string]bool),
		c2cSubs:   make(map[string]bool),
	}
}

// New 创建广播插件描述符
func New(cfg ...Config) *plugin.PluginDescriptor {
	c := DefaultConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	p := &Plugin{
		cfg:       c,
		rl:        rate.NewLimiter(rate.Limit(c.Rate), c.Burst),
		groupSubs: make(map[string]bool),
		c2cSubs:   make(map[string]bool),
	}

	return &plugin.PluginDescriptor{
		Name:    "broadcast",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "广播/推送插件，支持向多群和多用户批量发送消息",
			Category:    "核心",
			Tags:        []string{"广播", "推送", "通知"},
			HelpText: `广播插件使用说明：
  bc := plugin.Require[broadcast.Plugin](ctx, "broadcast")
  bc.SetAPI(api)
  bc.ToGroups(groupIDs, msg)
  bc.ToSubscribedGroups(msg)`,
		},
		Setup: func(setupCtx *plugin.SetupContext) (any, error) {
			setupCtx.Log.Infof("Plugin loaded (rate=%.1f/s concurrency=%d)", c.Rate, c.Concurrency)
			if storageRaw, ok := setupCtx.Get("storage"); ok {
				if sb, ok := storageRaw.(storageBackend); ok {
					p.storage = sb
					p.loadSubs()
				}
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).saveSubs()
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
		logger.Error("[Broadcast] " + ErrAPINotSet.Error())
		errs := make([]error, len(targets))
		for i := range errs {
			errs[i] = ErrAPINotSet
		}
		return Result{Total: len(targets), Failed: len(targets), Errors: errs}
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

// SubscribeGroup 将群加入广播订阅列表
func (p *Plugin) SubscribeGroup(groupOpenID string) {
	p.subMu.Lock()
	p.groupSubs[groupOpenID] = true
	p.subMu.Unlock()
	go p.saveSubs()
}

// UnsubscribeGroup 将群从订阅列表移除
func (p *Plugin) UnsubscribeGroup(groupOpenID string) {
	p.subMu.Lock()
	delete(p.groupSubs, groupOpenID)
	p.subMu.Unlock()
	go p.saveSubs()
}

// SubscribeC2C 将用户加入广播订阅列表
func (p *Plugin) SubscribeC2C(userOpenID string) {
	p.subMu.Lock()
	p.c2cSubs[userOpenID] = true
	p.subMu.Unlock()
	go p.saveSubs()
}

// UnsubscribeC2C 将用户从订阅列表移除
func (p *Plugin) UnsubscribeC2C(userOpenID string) {
	p.subMu.Lock()
	delete(p.c2cSubs, userOpenID)
	p.subMu.Unlock()
	go p.saveSubs()
}

// ListGroupSubscribers 返回所有已订阅的群 ID
func (p *Plugin) ListGroupSubscribers() []string {
	p.subMu.RLock()
	defer p.subMu.RUnlock()
	out := make([]string, 0, len(p.groupSubs))
	for id := range p.groupSubs {
		out = append(out, id)
	}
	return out
}

// ListC2CSubscribers 返回所有已订阅的用户 ID
func (p *Plugin) ListC2CSubscribers() []string {
	p.subMu.RLock()
	defer p.subMu.RUnlock()
	out := make([]string, 0, len(p.c2cSubs))
	for id := range p.c2cSubs {
		out = append(out, id)
	}
	return out
}

// ToSubscribedGroups 向所有已订阅的群发送消息
func (p *Plugin) ToSubscribedGroups(msg *dto.Message) Result {
	return p.ToGroups(p.ListGroupSubscribers(), msg)
}

// ToSubscribedC2C 向所有已订阅的用户发送私聊消息
func (p *Plugin) ToSubscribedC2C(msg *dto.Message) Result {
	return p.ToC2C(p.ListC2CSubscribers(), msg)
}

type subSnapshot struct {
	Groups []string `json:"groups"`
	C2Cs   []string `json:"c2cs"`
}

func (p *Plugin) saveSubs() {
	if p.storage == nil {
		return
	}
	snap := subSnapshot{
		Groups: p.ListGroupSubscribers(),
		C2Cs:   p.ListC2CSubscribers(),
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	_ = p.storage.Set("broadcast:subscriptions", data, 0)
}

func (p *Plugin) loadSubs() {
	if p.storage == nil {
		return
	}
	data, err := p.storage.Get("broadcast:subscriptions")
	if err != nil {
		return
	}
	var snap subSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	p.subMu.Lock()
	defer p.subMu.Unlock()
	for _, id := range snap.Groups {
		p.groupSubs[id] = true
	}
	for _, id := range snap.C2Cs {
		p.c2cSubs[id] = true
	}
	logger.Infof("[Broadcast] Loaded %d group + %d c2c subscriptions", len(snap.Groups), len(snap.C2Cs))
}
