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
//	pm.Register(antispam.New(antispam.Config{
//	    UserRate:   5, UserBurst: 8,
//	    GroupRate:  20, GroupBurst: 30,
//	    BanOnViolation: true, BanDuration: 5*time.Minute,
//	}))
//	// Handler 中：
//	spam := ctx.MustGet("antispam").(*antispam.Plugin)
//	engine.On(string(platform.EventKindGroupMessage), spam.Rule()).Handle(myHandler)
package antispam

import (
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"

	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/syncx"
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

// banEntryJSON 用于 JSON 序列化的封禁记录（time.Time 不直接 JSON 友好）
type banEntryJSON struct {
	Until int64 `json:"until"` // Unix 时间戳（纳秒），0 = 永久
}

// Plugin 反垃圾插件 API
type Plugin struct {
	cfg      Config
	userRL   *lru.Cache[string, *rate.Limiter]
	groupRL  *lru.Cache[string, *rate.Limiter]
	banList  syncx.Map[string, banEntry]
	dataFile string // 持久化文件路径（空字符串=纯内存）
}

// Option 配置选项
type Option func(*Plugin)

// WithDataFile 设置 JSON 持久化文件路径。空字符串表示纯内存模式。
func WithDataFile(path string) Option {
	return func(p *Plugin) { p.dataFile = path }
}

// NewPlugin 创建并返回一个已初始化的 AntiSpam Plugin 实例。
// 配合 p.Descriptor() 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := antispam.NewPlugin(antispam.DefaultConfig())
//	pm.Register(p.Descriptor())
//	engine.OnGroupAt(p.Rule())
func NewPlugin(cfg Config, opts ...Option) *Plugin {
	cfg = normalizeConfig(cfg)
	userCache, _ := lru.New[string, *rate.Limiter](50000)
	groupCache, _ := lru.New[string, *rate.Limiter](10000)
	p := &Plugin{
		cfg:     cfg,
		userRL:  userCache,
		groupRL: groupCache,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.Register 使用。
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "antispam",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "反垃圾/防刷插件，用户和群组独立限速，支持违规封禁",
			Category:    "核心",
			Tags:        []string{"安全", "防刷", "限速", "反垃圾"},
			HelpText: `反垃圾插件使用说明：
  p := antispam.NewPlugin(antispam.DefaultConfig())
  pm.Register(p.Descriptor())
  p.Ban(userID, 10*time.Minute)`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Infof("Loaded (user_rate=%.1f/s group_rate=%.1f/s ban_on_violation=%v)",
				p.cfg.UserRate, p.cfg.GroupRate, p.cfg.BanOnViolation)
			p.loadBanList()
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).saveBanList()
			return nil
		},
	}
}

// loadBanList 从 JSON 文件加载封禁名单
func (p *Plugin) loadBanList() {
	if p.dataFile == "" {
		return
	}
	entries, err := jsonfile.Read[map[string]banEntryJSON](p.dataFile)
	if err != nil {
		return
	}
	now := time.Now()
	for id, e := range entries {
		var until time.Time
		if e.Until != 0 {
			until = time.Unix(0, e.Until)
			if until.Before(now) {
				continue
			}
		}
		p.banList.Store(id, banEntry{until: until})
	}
	logger.Infof("[AntiSpam] Loaded %d ban entries", p.banList.Len())
}

// saveBanList 将封禁名单保存到 JSON 文件
func (p *Plugin) saveBanList() {
	if p.dataFile == "" {
		return
	}
	entries := make(map[string]banEntryJSON, p.banList.Len())
	now := time.Now()
	p.banList.Range(func(id string, e banEntry) bool {
		if !e.until.IsZero() && e.until.Before(now) {
			return true
		}
		entries[id] = banEntryJSON{Until: e.until.UnixNano()}
		return true
	})

	if err := jsonfile.Write(p.dataFile, entries); err != nil {
		logger.WithError(err).Warn("[AntiSpam] Failed to save ban list")
		return
	}
	logger.Infof("[AntiSpam] Saved %d ban entries", len(entries))
}

// BanEntry 封禁条目（公开查询用）
type BanEntry struct {
	UserID    string
	Until     time.Time // zero if permanent
	Permanent bool
}

// Stats 限流统计摘要
type Stats struct {
	BanCount          int
	UserLimiterCount  int
	GroupLimiterCount int
}

// ListBans 返回所有当前有效的封禁记录（过期的自动跳过）
func (p *Plugin) ListBans() []BanEntry {
	now := time.Now()
	var result []BanEntry
	var expired []string

	p.banList.Range(func(userID string, e banEntry) bool {
		if !e.until.IsZero() && e.until.Before(now) {
			expired = append(expired, userID) // 收集过期记录，稍后清理
			return true
		}
		result = append(result, BanEntry{
			UserID:    userID,
			Until:     e.until,
			Permanent: e.until.IsZero(),
		})
		return true
	})
	for _, id := range expired {
		p.banList.Delete(id)
	}
	return result
}

// Stats 返回限流统计摘要
func (p *Plugin) Stats() Stats {
	return Stats{
		BanCount:          p.banList.Len(),
		UserLimiterCount:  p.userRL.Len(),
		GroupLimiterCount: p.groupRL.Len(),
	}
}

// New 创建反垃圾插件描述符（便捷入口）。
// 若需要持有 Plugin 引用，改用 NewPlugin(cfg) + Descriptor()。
func New(cfg Config, opts ...Option) *plugin.Descriptor {
	return NewPlugin(cfg, opts...).Descriptor()
}

// Get 从插件管理器中获取已注册的 AntiSpam 插件实例（类型安全）。
// 需在 pm.Register(New(cfg)) 之后调用。
func Get(pm *plugin.Manager) *Plugin {
	v, ok := pm.GetContainer().Get("antispam")
	if !ok {
		panic("antispam: plugin not registered; call pm.Register(antispam.New(cfg)) first")
	}
	p, ok := v.(*Plugin)
	if !ok {
		panic("antispam: unexpected type in container")
	}
	return p
}

func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.UserRate <= 0 {
		cfg.UserRate = d.UserRate
	}
	if cfg.UserBurst <= 0 {
		cfg.UserBurst = d.UserBurst
	}
	if cfg.GroupRate <= 0 {
		cfg.GroupRate = d.GroupRate
	}
	if cfg.GroupBurst <= 0 {
		cfg.GroupBurst = d.GroupBurst
	}
	return cfg
}

// Rule 返回可直接传入 engine.On() / engine.OnGroupAt() 的规则函数。
// 规则按顺序检查：封禁名单 → 用户限速 → 群组限速。
//
// 支持所有平台（新路径和旧 QQ 路径均可用）。
func (p *Plugin) Rule() eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		userID := ctx.GetSenderInfo().ID

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

		// 3. 群组级限速（仅群组消息）
		if p.cfg.GroupRate > 0 {
			if e := ctx.GetPlatformEvent(); e != nil && e.Chat().IsGroup {
				groupID := e.Chat().ID
				if groupID != "" {
					rl := p.getGroupLimiter(groupID)
					if !rl.Allow() {
						p.handleViolation(ctx, groupID, "group rate limit exceeded")
						return false
					}
				}
			}
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
	var until time.Time
	if duration > 0 {
		until = time.Now().Add(duration)
	}
	p.banList.Store(userID, banEntry{until: until})
	logger.Infof("[AntiSpam] Banned user %s until %v", userID, until)
	go p.saveBanList() // 异步持久化
}

// Unban 解封用户
func (p *Plugin) Unban(userID string) {
	p.banList.Delete(userID)
	logger.Infof("[AntiSpam] Unbanned user %s", userID)
	go p.saveBanList() // 异步持久化
}

// IsBanned 检查用户是否被封禁（自动清理过期封禁）
func (p *Plugin) IsBanned(userID string) bool {
	var banned bool
	p.banList.Compute(userID, func(e banEntry, exists bool) (banEntry, bool) {
		if !exists {
			return e, false // key 不存在，Compute 中 delete 是空操作
		}
		if !e.until.IsZero() && time.Now().After(e.until) {
			return e, false // 已过期，删除并返回未封禁
		}
		banned = true
		return e, true // 有效封禁，保留
	})
	return banned
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
		userID := ctx.GetSenderInfo().ID
		if userID != "" {
			p.Ban(userID, p.cfg.BanDuration)
		}
	}
	if p.cfg.OnViolation != nil {
		p.cfg.OnViolation(ctx, reason)
	}
}
