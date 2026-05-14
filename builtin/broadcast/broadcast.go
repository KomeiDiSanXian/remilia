// Package broadcast 提供广播/推送插件。
//
// 功能：
//   - 向多个群或用户批量发送消息
//   - 内置发送速率控制（复用 sendqueue 或直接内置令牌桶）
//   - 发送结果统计（成功/失败数）
//   - 支持平台无关的 platform.Sender，兼容 QQ、Discord、Telegram 等所有平台
//
// 使用 SetSender 注入 platform.Sender，然后调用 Broadcast：
//
//	bc.SetSender(ctx.GetPlatformSender())
//	result := bc.Broadcast([]string{"chat001", "chat002"}, platform.TextMessage("公告"))
package broadcast

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"golang.org/x/time/rate"
)

// defaultBroadcastCtx 返回内部使用的 context。
// 当调用方未提供 ctx 时（如 BroadcastToGroups/BroadcastToUsers），使用 Background()。
var defaultBroadcastCtx = context.Background

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
	cfg    Config
	rl     *rate.Limiter
	mu     sync.RWMutex
	sender platform.Sender

	groupSubs map[string]bool
	c2cSubs   map[string]bool
	subMu     sync.RWMutex

	dataFile string // 持久化文件路径（空字符串=纯内存）
}

// Option 配置选项
type Option func(*Plugin)

// WithDataFile 设置 JSON 持久化文件路径。空字符串表示纯内存模式。
func WithDataFile(path string) Option {
	return func(p *Plugin) { p.dataFile = path }
}

// NewPlugin 创建 Plugin 实例
func NewPlugin(cfg Config, opts ...Option) *Plugin {
	p := &Plugin{
		cfg:       cfg,
		rl:        rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Burst),
		groupSubs: make(map[string]bool),
		c2cSubs:   make(map[string]bool),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// New 创建广播插件描述符
func New(cfg ...Config) *plugin.Descriptor {
	c := DefaultConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return NewPlugin(c).Descriptor()
}

// Descriptor 从已有 Plugin 创建描述符
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "broadcast",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "广播/推送插件，支持向多群和多用户批量发送消息",
			Category:    "核心",
			Tags:        []string{"广播", "推送", "通知"},
			HelpText: `广播插件使用说明：
  bc := plugin.Service[broadcast.Plugin](ctx, "broadcast")
  bc.SetSender(sender)
  bc.Broadcast(chatIDs, platform.TextMessage("公告"))`,
		},
		Setup: func(setupCtx *plugin.SetupContext) (any, error) {
			setupCtx.Log.Infof("Plugin loaded (rate=%.1f/s concurrency=%d)", p.cfg.Rate, p.cfg.Concurrency)
			p.loadSubs()
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

// Broadcast 向多个会话发送消息（平台无关）。
//
// chats 为目标会话列表（包含 ID 和 IsGroup 路由信息）。
// 每个目标会话的 ChatInfo 会注入到 Go context，平台 Sender 通过 ChatInfoFromContext 路由。
// 返回汇总的发送结果。
func (p *Plugin) Broadcast(ctx context.Context, chats []platform.ChatInfo, msg platform.OutboundMessage) Result {
	s := p.getSender()
	if s == nil {
		errs := make([]error, len(chats))
		for i := range errs {
			errs[i] = fmt.Errorf("broadcast: no sender set, call SetSender() first")
		}
		return Result{Total: len(chats), Failed: len(chats), Errors: errs}
	}

	result := Result{Total: len(chats)}
	var (
		mu      sync.Mutex
		success int64
		failed  int64
		errs    []error
		wg      sync.WaitGroup
		sem     = make(chan struct{}, p.cfg.Concurrency)
	)

	for _, chat := range chats {
		wg.Add(1)
		sem <- struct{}{}
		go func(c platform.ChatInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			_ = p.rl.Wait(ctx)
			req := platform.SendRequest{Target: c, Message: msg}
			if _, err := s.Send(ctx, req); err != nil {
				atomic.AddInt64(&failed, 1)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				logger.WithError(err).Warnf("[Broadcast] Failed to send to %s", c.ID)
			} else {
				atomic.AddInt64(&success, 1)
			}
		}(chat)
	}
	wg.Wait()
	result.Success = int(success)
	result.Failed = int(failed)
	result.Errors = errs
	logger.Infof("[Broadcast] Completed: total=%d success=%d failed=%d", result.Total, result.Success, result.Failed)
	return result
}

// BroadcastToGroups 向多个群组会话广播消息（SetSender 后调用）。
//
// groupIDs 为群组 ID 列表（如 QQ 群 openID、Discord channel ID 等）。
func (p *Plugin) BroadcastToGroups(groupIDs []string, msg platform.OutboundMessage) Result {
	chats := make([]platform.ChatInfo, len(groupIDs))
	for i, id := range groupIDs {
		chats[i] = platform.ChatInfo{ID: id, IsGroup: true}
	}
	return p.Broadcast(defaultBroadcastCtx(), chats, msg)
}

// BroadcastToGroupsWithContext 支持传入 context 的版本。
func (p *Plugin) BroadcastToGroupsWithContext(ctx context.Context, groupIDs []string, msg platform.OutboundMessage) Result {
	chats := make([]platform.ChatInfo, len(groupIDs))
	for i, id := range groupIDs {
		chats[i] = platform.ChatInfo{ID: id, IsGroup: true}
	}
	return p.Broadcast(ctx, chats, msg)
}

// BroadcastToUsers 向多个私聊用户广播消息（SetSender 后调用）。
//
// userIDs 为用户 ID 列表（如 QQ user_openid、Telegram user_id 等）。
func (p *Plugin) BroadcastToUsers(userIDs []string, msg platform.OutboundMessage) Result {
	chats := make([]platform.ChatInfo, len(userIDs))
	for i, id := range userIDs {
		chats[i] = platform.ChatInfo{ID: id, IsGroup: false}
	}
	return p.Broadcast(defaultBroadcastCtx(), chats, msg)
}

// BroadcastToUsersWithContext 支持传入 context 的版本。
func (p *Plugin) BroadcastToUsersWithContext(ctx context.Context, userIDs []string, msg platform.OutboundMessage) Result {
	chats := make([]platform.ChatInfo, len(userIDs))
	for i, id := range userIDs {
		chats[i] = platform.ChatInfo{ID: id, IsGroup: false}
	}
	return p.Broadcast(ctx, chats, msg)
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
	if p.dataFile == "" {
		return
	}
	snap := subSnapshot{
		Groups: p.ListGroupSubscribers(),
		C2Cs:   p.ListC2CSubscribers(),
	}
	if err := jsonfile.Write(p.dataFile, snap); err != nil {
		logger.WithError(err).Warn("[Broadcast] Failed to save subscriptions")
	}
}

func (p *Plugin) loadSubs() {
	if p.dataFile == "" {
		return
	}
	snap, err := jsonfile.Read[subSnapshot](p.dataFile)
	if err != nil {
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
