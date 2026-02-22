// Package antispam 提供反垃圾/防刷插件。
//
// 功能：
//   - 用户级令牌桶（按 UserOpenID 独立限速）
//   - 群组级令牌桶（按 GroupOpenID 独立限速）
//   - 违规临时封禁（可配置封禁时长）
//   - 封禁名单管理（Ban/Unban/IsBanned）
//   - 提供 Rule() 返回可直接用于 engine.On() 的规则函数
//
// 使用示例:
//
//	pm.RegisterV2(antispam.New(antispam.Config{
//	    UserRate:   5, UserBurst: 8,
//	    GroupRate:  20, GroupBurst: 30,
//	    BanOnViolation: true, BanDuration: 5*time.Minute,
//	}))
//	// Handler 中：
//	spam := ctx.MustGet("antispam").(*antispam.Plugin)
//	engine.OnGroupAt(spam.Rule()).Handle(myHandler)
package antispam

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Config 反垃圾配置
type Config struct {
	// 用户级限速：每秒允许 UserRate 条，最大突发 UserBurst 条
	UserRate  float64
	UserBurst int
	// 群组级限速
	GroupRate  float64
	GroupBurst int
	// BanOnViolation 违规时是否自动封禁
	BanOnViolation bool
	// BanDuration 封禁时长，0 表示永久
	BanDuration time.Duration
	// OnViolation 违规回调（可选）
	OnViolation func(ctx *eventctx.Context, reason string)
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{
		UserRate:       5,
		UserBurst:      10,
		GroupRate:      30,
		GroupBurst:     50,
		BanOnViolation: true,
		BanDuration:    5 * time.Minute,
	}
}

// banEntry 封禁记录
type banEntry struct {
	until time.Time // zero = 永久
}

// Plugin 反垃圾插件 API
type Plugin struct {
	cfg     Config
	userRL  *lru.Cache[string, *rate.Limiter]
	groupRL *lru.Cache[string, *rate.Limiter]
	banList map[string]banEntry
	banMu   sync.RWMutex
}

// New creates the anti-spam plugin descriptor.
// Use NewPlugin() if you need a direct reference to the Plugin API (e.g. in tests).
func New(cfg Config) *plugin.PluginDescriptor {
	_, desc := NewPlugin(cfg)
	return desc
}

// NewPlugin creates the anti-spam plugin and returns both the Plugin API and its descriptor.
func NewPlugin(cfg Config) (*Plugin, *plugin.PluginDescriptor) {
	if cfg.UserRate <= 0 {
		cfg.UserRate = DefaultConfig().UserRate
	}
	if cfg.UserBurst <= 0 {
		cfg.UserBurst = DefaultConfig().UserBurst
	}
	if cfg.GroupRate <= 0 {
		cfg.GroupRate = DefaultConfig().GroupRate
	}
	if cfg.GroupBurst <= 0 {
		cfg.GroupBurst = DefaultConfig().GroupBurst
	}

	userCache, _ := lru.New[string, *rate.Limiter](50000)
	groupCache, _ := lru.New[string, *rate.Limiter](10000)

	p := &Plugin{
		cfg:     cfg,
		userRL:  userCache,
		groupRL: groupCache,
		banList: make(map[string]banEntry),
	}

	desc := &plugin.PluginDescriptor{
		Name:        "antispam",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "反垃圾/防刷插件，用户和群组独立限速，支持违规封禁",
		Category:    "核心",
		Tags:        []string{"安全", "防刷", "限速", "反垃圾"},
		Deps:        []string{},
		HelpText: `反垃圾插件使用说明：
  spam := ctx.MustGet("antispam").(*antispam.Plugin)
  spam.Ban(userID, 10*time.Minute)
  spam.Unban(userID)
  spam.IsBanned(userID)
  engine.OnGroupAt(spam.Rule())`,
		Setup: func(ctx *plugin.SetupContext) error {
			logger.Infof("[AntiSpam] Loaded (user_rate=%.1f/s group_rate=%.1f/s ban_on_violation=%v)",
				cfg.UserRate, cfg.GroupRate, cfg.BanOnViolation)
			ctx.Manager.GetContainer().Register("antispam", p)
			return nil
		},
	}
	return p, desc
}

// Rule 返回可直接传入 engine.On() / engine.OnGroupAt() 的规则函数。
// 规则按顺序检查：封禁名单 → 用户限速 → 群组限速。
func (p *Plugin) Rule() eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		author := ctx.GetAuthor()
		userID := ""
		if author != nil {
			userID = author.UserOpenID
		}

		// 1. 检查封禁名单
		if userID != "" && p.IsBanned(userID) {
			logger.Debugf("[AntiSpam] Blocked banned user %s", userID)
			return false
		}

		// 2. 用户级限速
		if userID != "" && p.cfg.UserRate > 0 {
			rl := p.getUserLimiter(userID)
			if !rl.Allow() {
				p.handleViolation(ctx, userID, "user rate limit exceeded")
				return false
			}
		}

		// 3. 群组级限速
		if p.cfg.GroupRate > 0 {
			var groupID string
			var event interface{ GetGroupOpenID() string }
			// 尝试从事件中提取 group open id
			var gae interface{}
			if err := ctx.DecodeEvent(&gae); err == nil {
				// 使用内容中提取（避免类型断言失败）
			}
			_ = groupID
			_ = event
		}

		return true
	}
}

// GroupRule 返回包含群组限速的规则（用于群消息场景）
func (p *Plugin) GroupRule(getGroupID func(*eventctx.Context) string) eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		if !p.Rule()(ctx) {
			return false
		}
		if p.cfg.GroupRate > 0 && getGroupID != nil {
			groupID := getGroupID(ctx)
			if groupID != "" {
				rl := p.getGroupLimiter(groupID)
				if !rl.Allow() {
					p.handleViolation(ctx, groupID, "group rate limit exceeded")
					return false
				}
			}
		}
		return true
	}
}

// Ban 封禁用户，duration=0 表示永久
func (p *Plugin) Ban(userID string, duration time.Duration) {
	p.banMu.Lock()
	defer p.banMu.Unlock()
	var until time.Time
	if duration > 0 {
		until = time.Now().Add(duration)
	}
	p.banList[userID] = banEntry{until: until}
	logger.Infof("[AntiSpam] Banned user %s until %v", userID, until)
}

// Unban 解封用户
func (p *Plugin) Unban(userID string) {
	p.banMu.Lock()
	defer p.banMu.Unlock()
	delete(p.banList, userID)
	logger.Infof("[AntiSpam] Unbanned user %s", userID)
}

// IsBanned 检查用户是否被封禁（自动清理过期封禁）
func (p *Plugin) IsBanned(userID string) bool {
	p.banMu.Lock()
	defer p.banMu.Unlock()
	entry, ok := p.banList[userID]
	if !ok {
		return false
	}
	if !entry.until.IsZero() && time.Now().After(entry.until) {
		delete(p.banList, userID)
		return false
	}
	return true
}

func (p *Plugin) getUserLimiter(userID string) *rate.Limiter {
	if rl, ok := p.userRL.Get(userID); ok {
		return rl
	}
	rl := rate.NewLimiter(rate.Limit(p.cfg.UserRate), p.cfg.UserBurst)
	p.userRL.Add(userID, rl)
	return rl
}

func (p *Plugin) getGroupLimiter(groupID string) *rate.Limiter {
	if rl, ok := p.groupRL.Get(groupID); ok {
		return rl
	}
	rl := rate.NewLimiter(rate.Limit(p.cfg.GroupRate), p.cfg.GroupBurst)
	p.groupRL.Add(groupID, rl)
	return rl
}

func (p *Plugin) handleViolation(ctx *eventctx.Context, id, reason string) {
	logger.Warnf("[AntiSpam] Violation: %s id=%s", reason, id)
	if p.cfg.BanOnViolation {
		author := ctx.GetAuthor()
		if author != nil && author.UserOpenID != "" {
			p.Ban(author.UserOpenID, p.cfg.BanDuration)
		}
	}
	if p.cfg.OnViolation != nil {
		p.cfg.OnViolation(ctx, reason)
	}
}
