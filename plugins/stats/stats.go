// Package stats 提供用户行为统计插件。
//
// 功能：
//   - 自动记录命令调用次数（通过中间件）
//   - 记录活跃用户（按日/周/月统计 UV）
//   - 查询 API：TopCommands / ActiveUsers / CommandCount
//   - 数据存储在内存中（可选对接 storage 插件持久化）
//
// 使用示例:
//
//	pm.RegisterV2(stats.New())
//	// 挂载中间件：
//	engine.Use(statsPlugin.Middleware())
//	// 查询：
//	sp := ctx.MustGet("stats").(*stats.Plugin)
//	top := sp.TopCommands(10)
package stats

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// CommandStat 命令统计
type CommandStat struct {
	Command string
	Count   int64
}

// UserStat 用户统计
type UserStat struct {
	UserID   string
	LastSeen time.Time
	MsgCount int64
}

// TimeWindow 统计时间窗口
type TimeWindow int

const (
	Today TimeWindow = iota
	Last7Days
	Last30Days
	AllTime
)

// Plugin 用户行为统计插件 API
type Plugin struct {
	commandCounts sync.Map // command -> *atomic.Int64
	userStats     sync.Map // userID -> *userEntry
	totalMessages atomic.Int64
}

type userEntry struct {
	mu       sync.Mutex
	lastSeen time.Time
	count    int64
}

// New creates the stats plugin descriptor.
// Use NewPlugin() if you need a direct reference to the Plugin API (e.g. in tests).
func New() *plugin.PluginDescriptor {
	_, desc := NewPlugin()
	return desc
}

// NewPlugin creates the stats plugin and returns both the Plugin API and its descriptor.
func NewPlugin() (*Plugin, *plugin.PluginDescriptor) {
	p := &Plugin{}
	desc := &plugin.PluginDescriptor{
		Name:        "stats",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "用户行为统计插件，记录命令调用次数和用户活跃度",
		Category:    "核心",
		Tags:        []string{"统计", "分析", "监控"},
		Deps:        []string{},
		HelpText: `统计插件使用说明：
  engine.Use(statsPlugin.Middleware())
  sp := ctx.MustGet("stats").(*stats.Plugin)
  top := sp.TopCommands(10)
  active := sp.ActiveUsers(stats.Today)
  total := sp.TotalMessages()`,
		Setup: func(setupCtx *plugin.SetupContext) error {
			logger.Info("[Stats] Plugin loaded")
			setupCtx.Manager.GetContainer().Register("stats", p)
			return nil
		},
	}
	return p, desc
}

// Middleware 返回自动统计中间件，应挂载到 engine
func (p *Plugin) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			// 统计消息
			p.totalMessages.Add(1)

			// 统计用户
			if author := ctx.GetAuthor(); author != nil && author.UserOpenID != "" {
				p.recordUser(author.UserOpenID)
			}

			// 统计命令（检测内容是否以 / 开头）
			content := strings.TrimSpace(ctx.GetMessageContent())
			if strings.HasPrefix(content, "/") {
				fields := strings.Fields(content)
				if len(fields) > 0 {
					p.recordCommand(fields[0])
				}
			}

			return next(ctx)
		}
	}
}

// RecordCommand 手动记录命令调用
func (p *Plugin) RecordCommand(command string) {
	p.recordCommand(command)
}

func (p *Plugin) recordCommand(cmd string) {
	v, _ := p.commandCounts.LoadOrStore(cmd, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

func (p *Plugin) recordUser(userID string) {
	v, _ := p.userStats.LoadOrStore(userID, &userEntry{})
	entry := v.(*userEntry)
	entry.mu.Lock()
	entry.lastSeen = time.Now()
	entry.count++
	entry.mu.Unlock()
}

// TopCommands 返回调用次数最多的 n 个命令
func (p *Plugin) TopCommands(n int) []CommandStat {
	var stats []CommandStat
	p.commandCounts.Range(func(k, v any) bool {
		stats = append(stats, CommandStat{
			Command: k.(string),
			Count:   v.(*atomic.Int64).Load(),
		})
		return true
	})
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Count > stats[j].Count
	})
	if n > 0 && len(stats) > n {
		stats = stats[:n]
	}
	return stats
}

// CommandCount 返回指定命令的调用次数
func (p *Plugin) CommandCount(command string) int64 {
	if v, ok := p.commandCounts.Load(command); ok {
		return v.(*atomic.Int64).Load()
	}
	return 0
}

// ActiveUsers 返回在时间窗口内活跃的用户列表
func (p *Plugin) ActiveUsers(window TimeWindow) []UserStat {
	cutoff := cutoffTime(window)
	var result []UserStat
	p.userStats.Range(func(k, v any) bool {
		entry := v.(*userEntry)
		entry.mu.Lock()
		lastSeen := entry.lastSeen
		count := entry.count
		entry.mu.Unlock()
		if cutoff.IsZero() || lastSeen.After(cutoff) {
			result = append(result, UserStat{
				UserID:   k.(string),
				LastSeen: lastSeen,
				MsgCount: count,
			})
		}
		return true
	})
	sort.Slice(result, func(i, j int) bool {
		return result[i].MsgCount > result[j].MsgCount
	})
	return result
}

// TotalMessages 返回总消息数
func (p *Plugin) TotalMessages() int64 {
	return p.totalMessages.Load()
}

// Reset 重置所有统计数据
func (p *Plugin) Reset() {
	p.commandCounts = sync.Map{}
	p.userStats = sync.Map{}
	p.totalMessages.Store(0)
}

func cutoffTime(w TimeWindow) time.Time {
	now := time.Now()
	switch w {
	case Today:
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case Last7Days:
		return now.Add(-7 * 24 * time.Hour)
	case Last30Days:
		return now.Add(-30 * 24 * time.Hour)
	default: // AllTime
		return time.Time{}
	}
}
