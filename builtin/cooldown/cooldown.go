// Package cooldown 提供命令冷却时间插件。
//
// 比 antispam 更轻量，专注于单命令冷却控制。
// 支持用户级和全局冷却时间，可作为中间件或规则使用。
//
// 使用示例:
//
//	pm.RegisterV2(cooldown.New())
//
//	// 作为中间件（在 Setup 中）：
//	cd := ctx.MustGet("cooldown").(*cooldown.Plugin)
//	engine.OnCommand(dto.C2CMessageCreate, "/daily").
//	    Use(cd.Middleware("daily", 24*time.Hour)).
//	    Handle(dailyHandler)
//
//	// 手动检查（在 Handler 中）：
//	cd := ctx.MustGet("cooldown").(*cooldown.Plugin)
//	if !cd.Allow(userID, "sign", 24*time.Hour) {
//	    remaining := cd.Remaining(userID, "sign", 24*time.Hour)
//	    return ctx.Reply(platform.TextMessage(fmt.Sprintf("冷却中，还需等待 %s", remaining.Round(time.Second))))
//	}
package cooldown

import (
	stdctx "context"
	"fmt"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
}

// Plugin 冷却时间插件 API
type Plugin struct {
	mu      sync.RWMutex
	records map[string]*entry // key: "userID:command"
}

// NewPlugin 创建 Plugin 实例
func NewPlugin() *Plugin {
	return &Plugin{
		records: make(map[string]*entry),
	}
}

// New 创建冷却时间插件描述符（便捷入口）。
// 若需要在注册前持有 Plugin 引用（如测试），改用 NewPlugin() + Descriptor()。
func New() *plugin.PluginDescriptor {
	return Descriptor(NewPlugin())
}

// Descriptor 从已有 Plugin 创建描述符
func Descriptor(p *Plugin) *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:    "cooldown",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "命令冷却时间插件，支持用户级和命令级冷却控制",
			Category:    "核心",
			Tags:        []string{"冷却", "限速", "防刷"},
			HelpText: `冷却时间插件使用说明：
  p := cooldown.NewPlugin()
  pm.RegisterV2(cooldown.Descriptor(p))
  engine.OnCommand(...).Use(p.Middleware("cmd", 10*time.Second)).Handle(h)`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			// 后台定期清理过期记录，防止 map 无限增长（Bug 2.2 修复）
			ctx.Go(func(runCtx stdctx.Context) {
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
			})
			return p, nil
		},
	}
}

// cdKey 构建冷却记录 key
func cdKey(userID, command string) string {
	return userID + ":" + command
}

// Allow 检查用户是否允许执行命令（冷却时间已到则重置计时并返回 true）
func (p *Plugin) Allow(userID, command string, cooldown time.Duration) bool {
	key := cdKey(userID, command)
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	e, exists := p.records[key]
	if !exists || now.Sub(e.lastUsed) >= cooldown {
		p.records[key] = &entry{lastUsed: now}
		return true
	}
	return false
}

// Remaining 返回还需等待的时间（如果冷却已过则返回 0）
func (p *Plugin) Remaining(userID, command string, cooldown time.Duration) time.Duration {
	key := cdKey(userID, command)

	p.mu.RLock()
	defer p.mu.RUnlock()

	e, exists := p.records[key]
	if !exists {
		return 0
	}
	remaining := cooldown - time.Since(e.lastUsed)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset 重置用户对某命令的冷却时间
func (p *Plugin) Reset(userID, command string) {
	key := cdKey(userID, command)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.records, key)
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
				_ = ctx.Reply(platform.TextMessage(msg))
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

// CleanExpired 清理已过期的冷却记录（减少内存占用）
func (p *Plugin) CleanExpired(maxAge time.Duration) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	threshold := time.Now().Add(-maxAge)
	count := 0
	for key, e := range p.records {
		if e.lastUsed.Before(threshold) {
			delete(p.records, key)
			count++
		}
	}
	return count
}

// Record 单条冷却记录（供查询使用）
type Record struct {
	Command  string
	LastUsed time.Time
}

// QueryUser 返回指定用户的所有冷却记录
func (p *Plugin) QueryUser(userID string) []Record {
	prefix := userID + ":"
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]Record, 0)
	for key, e := range p.records {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, Record{
				Command:  key[len(prefix):],
				LastUsed: e.lastUsed,
			})
		}
	}
	return result
}

// ActiveCount 返回当前活跃冷却记录总数
func (p *Plugin) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.records)
}
