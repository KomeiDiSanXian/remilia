// Package cooldown 提供命令冷却时间插件。
//
// 比 antispam 更轻量，专注于单命令冷却控制。
// 支持用户级和全局冷却时间，可作为中间件或规则使用。
//
// 使用示例:
//
//	pm.Register(cooldown.New())
//
//	// 作为中间件（在 Setup 中）：
//	cdSvc := plugin.Service[*cooldown.Plugin](ctx, "cooldown")
//	engine.OnCommand(dto.C2CMessageCreate, "/daily").
//	    Use(cd.Middleware("daily", 24*time.Hour)).
//	    Handle(dailyHandler)
//
//	// 手动检查（在 Handler 中）：
//	cdSvc := plugin.Service[*cooldown.Plugin](ctx, "cooldown")
//	if !cd.Allow(userID, "sign", 24*time.Hour) {
//	    remaining := cd.Remaining(userID, "sign", 24*time.Hour)
//	    return ctx.Reply(platform.TextMessage(fmt.Sprintf("冷却中，还需等待 %s", remaining.Round(time.Second))))
//	}
package cooldown

import (
	stdctx "context"
	"fmt"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/syncx"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const (
	// cleanupInterval 后台 GC goroutine 的运行间隔
	cleanupInterval = 5 * time.Minute
	// maxEntryAge 超过此时间未使用的冷却记录视为过期并清理
	// 设为常见最大冷却时间的 2 倍（24h），确保已过期但从未被访问的记录被回收
	maxEntryAge = 24 * time.Hour
)

// entry 冷却记录
type entry struct {
	lastUsed time.Time
	data     any // 可选的附加数据（由 AllowWithData 存储）
	// cooldown 记录本条目对应的冷却时长，供 GC 判断能否安全回收。
	// 若不记录，固定 24h 的 GC 会提前删除冷却期更长的记录，
	// 而 Allow 把"键不存在"视为放行，冷却因此被绕过
	// （例如 7 天领奖冷却，次日即可重复领取）。
	cooldown time.Duration
}

// Plugin 冷却时间插件 API
type Plugin struct {
	records syncx.Map[string, *entry] // key: "userID:command"
}

// NewPlugin 创建 Plugin 实例
func NewPlugin() *Plugin {
	return &Plugin{}
}

// New 创建冷却时间插件描述符（便捷入口）。
// 若需要在注册前持有 Plugin 引用（如测试），改用 NewPlugin() + p.Descriptor()。
func New() *plugin.Descriptor {
	return NewPlugin().Descriptor()
}

// Descriptor 从已有 Plugin 创建描述符
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "cooldown",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "命令冷却时间插件，支持用户级和命令级冷却控制",
			Category:    "核心",
			Tags:        []string{"冷却", "限速", "防刷"},
			HelpText: `冷却时间插件使用说明：
  p := cooldown.NewPlugin()
  pm.Register(p.Descriptor())
  engine.OnCommand(...).Use(p.Middleware("cmd", 10*time.Second)).Handle(h)`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			// 后台定期清理过期记录，防止 map 无限增长（Bug 2.2 修复）
			var fn = func(runCtx stdctx.Context) {
				ticker := time.NewTicker(cleanupInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						removed := p.CleanExpired(maxEntryAge)
						if removed > 0 {
							ctx.Log.Infof("GC: removed %d expired cooldown entries", removed)
						}
					case <-runCtx.Done():
						return
					}
				}
			}
			ctx.Spawn(fn)
			return p, nil
		},
	}
}

// cdKey 构建冷却记录 key
func cdKey(userID, command string) string {
	return userID + ":" + command
}

// Allow 检查用户是否允许执行命令（冷却时间已到则重置计时并返回 true）
func (p *Plugin) Allow(userID, command string, cooldownDur time.Duration) bool {
	key := cdKey(userID, command)
	now := time.Now()
	var allowed bool
	p.records.Compute(key, func(e *entry, exists bool) (*entry, bool) {
		if !exists || now.Sub(e.lastUsed) >= cooldownDur {
			allowed = true
			return &entry{lastUsed: now, cooldown: cooldownDur}, true
		}
		return e, true
	})
	return allowed
}

// AllowWithData 与 Allow 相同，但在允许时同时存储 data。
//
// 当冷却期内再次调用时返回 false，data 参数被忽略（已存储的 data 不变）。
// 典型用法：存储上次操作的结果，以便在冷却期内回显。
//
//	if p.AllowWithData(uid, "omikuji", 24*time.Hour, result) {
//	    // 新的一次
//	} else {
//	    prev, _ := p.GetData(uid, "omikuji")
//	    // 展示上次结果
//	}
func (p *Plugin) AllowWithData(userID, command string, cooldownDur time.Duration, data any) bool {
	key := cdKey(userID, command)
	now := time.Now()
	var allowed bool
	p.records.Compute(key, func(e *entry, exists bool) (*entry, bool) {
		if !exists || now.Sub(e.lastUsed) >= cooldownDur {
			allowed = true
			return &entry{lastUsed: now, data: data, cooldown: cooldownDur}, true
		}
		return e, true
	})
	return allowed
}

// GetData 返回上次 AllowWithData 存储的附加数据。
//
// 若冷却记录不存在（从未允许过），返回 (nil, false)。
func (p *Plugin) GetData(userID, command string) (any, bool) {
	key := cdKey(userID, command)
	e, exists := p.records.Load(key)
	if !exists {
		return nil, false
	}
	return e.data, true
}

// Remaining 返回还需等待的时间（如果冷却已过则返回 0）
func (p *Plugin) Remaining(userID, command string, cooldownDur time.Duration) time.Duration {
	key := cdKey(userID, command)
	e, exists := p.records.Load(key)
	if !exists {
		return 0
	}
	remaining := cooldownDur - time.Since(e.lastUsed)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset 重置用户对某命令的冷却时间
func (p *Plugin) Reset(userID, command string) {
	p.records.Delete(cdKey(userID, command))
}

// Middleware 返回可用于 engine.OnXxx().Use() 的冷却时间中间件
func (p *Plugin) Middleware(command string, duration time.Duration) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			userID := ctx.GetSenderInfo().ID
			if userID == "" {
				return next(ctx)
			}

			if !p.Allow(userID, command, duration) {
				remaining := p.Remaining(userID, command, duration)
				logger.Debugf("[Cooldown] User %s is in cooldown for %s, remaining: %s", userID, command, remaining.Round(time.Second))
				msg := fmt.Sprintf("⏱ 操作太频繁，请在 %s 后再试", remaining.Round(time.Second))
				ctx.Reply(platform.TextMessage(msg))
				return nil
			}
			return next(ctx)
		}
	}
}

// GlobalAllow 检查全局（不区分用户）命令冷却时间
func (p *Plugin) GlobalAllow(command string, cooldown time.Duration) bool {
	return p.Allow("__global__", command, cooldown)
}

// ─── 群级冷却 ─────────────────────────────────────────────────────────────────

// GroupAllow 检查群组是否允许执行命令（群内所有用户共享冷却时间）。
//
// 与 Allow 不同，这里的 key 是群 ID 而非用户 ID，
// 因此任何一个群成员触发命令后，该群整体进入冷却状态。
func (p *Plugin) GroupAllow(groupID, command string, cooldown time.Duration) bool {
	return p.Allow("__group__:"+groupID, command, cooldown)
}

// GroupRemaining 返回指定群组的命令冷却剩余时间（已过则返回 0）。
func (p *Plugin) GroupRemaining(groupID, command string, cooldown time.Duration) time.Duration {
	return p.Remaining("__group__:"+groupID, command, cooldown)
}

// GroupReset 重置指定群组对某命令的冷却时间。
func (p *Plugin) GroupReset(groupID, command string) {
	p.Reset("__group__:"+groupID, command)
}

// GroupMiddleware 返回群级冷却时间中间件。
//
// 与 [Plugin.Middleware] 不同，冷却时间在整个群内共享：
// 只要群内任意用户触发了该命令，整个群进入冷却状态（直到冷却结束）。
// 非群组消息（私聊）直接放行，不受此中间件影响。
//
// 使用示例：
//
//	engine.OnCommand(dto.GroupMessage, "/news").
//	    Use(cd.GroupMiddleware("news", 10*time.Minute)).
//	    Handle(newsHandler)
func (p *Plugin) GroupMiddleware(command string, duration time.Duration) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			chat := ctx.GetChatInfo()
			if chat.ID == "" || !chat.IsGroup {
				// 私聊：不应用群级冷却，直接放行
				return next(ctx)
			}
			if !p.GroupAllow(chat.ID, command, duration) {
				remaining := p.GroupRemaining(chat.ID, command, duration)
				logger.Debugf("[Cooldown] Group %s is in cooldown for %s, remaining: %s",
					chat.ID, command, remaining.Round(time.Second))
				msg := fmt.Sprintf("⏱ 该群冷却中，请在 %s 后再试", remaining.Round(time.Second))
				ctx.Reply(platform.TextMessage(msg))
				return nil
			}
			return next(ctx)
		}
	}
}

// ─── 冷却策略 ─────────────────────────────────────────────────────────────────

// Policy 描述一条命令的复合冷却策略，支持用户级、群级、全局三层限制。
//
// 三层按 GlobalLimit → GroupLimit → UserLimit 顺序检查（从最严到最细），
// 任一层冷却中则拒绝，并告知用户相应剩余时间。
//
// 使用示例：
//
//	policy := cooldown.Policy{
//	    Command:     "sign",
//	    UserLimit:   24 * time.Hour,  // 每用户每天签到一次
//	    GroupLimit:  0,               // 群级不限制
//	    GlobalLimit: 0,               // 全局不限制
//	}
//	engine.OnCommand(dto.GroupMessage, "/sign").
//	    Use(cd.PolicyMiddleware(policy)).
//	    Handle(signHandler)
type Policy struct {
	// Command 命令名，用作冷却记录的 key（应与命令唯一标识一致）
	Command string
	// UserLimit 用户级冷却时间（0 表示不限制）
	UserLimit time.Duration
	// GroupLimit 群级冷却时间（0 表示不限制）
	GroupLimit time.Duration
	// GlobalLimit 全局冷却时间（0 表示不限制）
	GlobalLimit time.Duration
}

// PolicyMiddleware 根据 Policy 生成复合冷却中间件。
//
// 检查顺序：全局 → 群组 → 用户，任一层冷却则中断并回复提示。
func (p *Plugin) PolicyMiddleware(policy Policy) eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			// 1. 全局冷却检查
			if policy.GlobalLimit > 0 {
				if !p.GlobalAllow(policy.Command, policy.GlobalLimit) {
					remaining := p.Remaining("__global__", policy.Command, policy.GlobalLimit)
					msg := fmt.Sprintf("⏱ 全局冷却中，请在 %s 后再试", remaining.Round(time.Second))
					ctx.Reply(platform.TextMessage(msg))
					return nil
				}
			}
			// 2. 群级冷却检查
			if policy.GroupLimit > 0 {
				chat := ctx.GetChatInfo()
				if chat.IsGroup && chat.ID != "" {
					if !p.GroupAllow(chat.ID, policy.Command, policy.GroupLimit) {
						remaining := p.GroupRemaining(chat.ID, policy.Command, policy.GroupLimit)
						msg := fmt.Sprintf("⏱ 该群冷却中，请在 %s 后再试", remaining.Round(time.Second))
						ctx.Reply(platform.TextMessage(msg))
						return nil
					}
				}
			}
			// 3. 用户级冷却检查
			if policy.UserLimit > 0 {
				userID := ctx.GetSenderInfo().ID
				if userID != "" {
					if !p.Allow(userID, policy.Command, policy.UserLimit) {
						remaining := p.Remaining(userID, policy.Command, policy.UserLimit)
						logger.Debugf("[Cooldown] User %s is in cooldown for %s, remaining: %s",
							userID, policy.Command, remaining.Round(time.Second))
						msg := fmt.Sprintf("⏱ 操作太频繁，请在 %s 后再试", remaining.Round(time.Second))
						ctx.Reply(platform.TextMessage(msg))
						return nil
					}
				}
			}
			return next(ctx)
		}
	}
}

// CleanExpired 清理已过期的冷却记录（减少内存占用）
func (p *Plugin) CleanExpired(maxAge time.Duration) int {
	now := time.Now()
	var toDelete []string
	p.records.Range(func(key string, e *entry) bool {
		// 取 maxAge 与该条目自身冷却时长中的较大者作为回收门槛：
		// 只有冷却确实已经结束（此时删除与保留对 Allow 的结果等价）才回收，
		// 避免把仍在生效的长冷却记录删掉造成绕过。
		age := max(e.cooldown, maxAge)
		if now.Sub(e.lastUsed) > age {
			toDelete = append(toDelete, key)
		}
		return true
	})
	for _, k := range toDelete {
		p.records.Delete(k)
	}
	return len(toDelete)
}

// Record 单条冷却记录（供查询使用）
type Record struct {
	Command  string
	LastUsed time.Time
}

// QueryUser 返回指定用户的所有冷却记录
func (p *Plugin) QueryUser(userID string) []Record {
	prefix := userID + ":"
	var result []Record
	p.records.Range(func(key string, e *entry) bool {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, Record{
				Command:  key[len(prefix):],
				LastUsed: e.lastUsed,
			})
		}
		return true
	})
	return result
}

// ActiveCount 返回当前活跃冷却记录总数
func (p *Plugin) ActiveCount() int {
	return p.records.Len()
}
