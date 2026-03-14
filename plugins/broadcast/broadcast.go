// Package broadcast 提供广播/推送插件。
//
// 功能：
//   - 向多个群或用户批量发送消息
//   - 内置发送速率控制（复用 sendqueue 或直接内置令牌桶）
//   - 发送结果统计（成功/失败数）
//   - 支持平台无关的 platform.Sender（新路径）和 openapi.OpenAPI（旧 QQ 路径）
//
// 推荐使用 SetSender 注入 platform.Sender，兼容所有平台：
//
//	bc.SetSender(ctx.GetPlatformSender())
//	result := bc.Broadcast([]string{"chat001", "chat002"}, platform.TextMessage("公告"))
//
// 旧 QQ 路径（仍然有效）：
//
//	bc.SetAPI(ctx.GetAPI())
//	result := bc.ToGroups([]string{"group1"}, dto.TextMsg("公告"))
package broadcast

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/time/rate"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
	storage "github.com/KomeiDiSanXian/remilia/plugins/core/storage"
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
	mu  sync.RWMutex

	// 平台无关发送器
	sender platform.Sender

	// 订阅管理
	groupSubs map[string]bool // groupOpenID -> subscribed
	c2cSubs   map[string]bool // userOpenID -> subscribed
	subMu     sync.RWMutex

	// 可选持久化后端
	storage storage.Client
}

// storageBackend 接口已合并至 storage.Client，见 plugins/core/storage

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
  bc := plugin.Must[broadcast.Plugin](ctx, "broadcast")
  bc.SetSender(sender)
  bc.Broadcast(chatIDs, platform.TextMessage("公告"))`,
		},
		Setup: func(setupCtx *plugin.SetupContext) (any, error) {
			setupCtx.Log.Infof("Plugin loaded (rate=%.1f/s concurrency=%d)", c.Rate, c.Concurrency)
			if sb, ok := plugin.Try[storage.Plugin](setupCtx, "storage"); ok {
				p.storage = sb
				p.loadSubs()
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).saveSubs()
			return nil
		},
	}
}

// SetSender 设置平台无关消息发送器（推荐使用）
func (p *Plugin) SetSender(s platform.Sender) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sender = s
}

// getSender 线程安全地获取平台发送器
func (p *Plugin) getSender() platform.Sender {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sender
}

// Broadcast 向多个会话发送消息（平台无关新路径）
//
// chatIDs 为目标会话 ID（群 ID 或用户 ID，取决于平台）。
// 返回汇总的发送结果。
func (p *Plugin) Broadcast(chatIDs []string, msg platform.OutboundMessage) Result {
	s := p.getSender()
	if s == nil {
		errs := make([]error, len(chatIDs))
		for i := range errs {
			errs[i] = fmt.Errorf("broadcast: no sender set, call SetSender() first")
		}
		return Result{Total: len(chatIDs), Failed: len(chatIDs), Errors: errs}
	}

	result := Result{Total: len(chatIDs)}
	var (
		mu      sync.Mutex
		success int64
		failed  int64
		errs    []error
		wg      sync.WaitGroup
		sem     = make(chan struct{}, p.cfg.Concurrency)
	)

	ctx := context.Background()
	for _, chatID := range chatIDs {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			_ = p.rl.Wait(ctx)
			if err := s.Send(ctx, id, msg); err != nil {
				atomic.AddInt64(&failed, 1)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				logger.WithError(err).Warnf("[Broadcast] Failed to send to %s", id)
			} else {
				atomic.AddInt64(&success, 1)
			}
		}(chatID)
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
