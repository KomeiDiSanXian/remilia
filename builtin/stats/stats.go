// Package stats 提供用户行为统计插件。
//
// # 与 stats/（根目录）的区别
//
// Remilia 统计相关代码分为两个层次：
//
//	stats/         — 基础统计原语，零外部依赖，供框架内部使用
//	plugins/stats/ — 用户行为统计插件（本包），基于插件系统
//
// ## plugins/stats/（本包）
//
// 面向 **Bot 业务层**，统计用户与 Bot 的交互行为：
//   - 自动记录命令调用次数（通过 Middleware() 挂载）
//   - 记录活跃用户（按日/周/月统计 UV）
//   - 查询 API：TopCommands / ActiveUsers / CommandCount
//   - 数据存储在内存中，可选对接 storage 插件持久化
//
// ## stats/（基础原语）
//
// 提供 Counter、Gauge、Histogram、QuantileHistogram 等线程安全数据结构，
// 供 middleware/adaptive.go 等框架内部组件使用，不涉及业务语义。
// 参见：github.com/KomeiDiSanXian/remilia/stats
//
// # 使用示例
//
//	pm.Register(stats.New())
//	// 挂载中间件：
//	engine.Use(statsPlugin.Middleware())
//	// 查询：
//	sp := ctx.MustGet("stats").(*stats.Plugin)
//	top := sp.TopCommands(10)
package stats

import (
	"context"
	stdctx "context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	storage "github.com/KomeiDiSanXian/remilia/builtin/core/storage"
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
	store         *storage.Store // 可选持久化后端
}

// storageBackend 接口已合并至 storage.Client，见 plugins/core/storage

type userEntry struct {
	mu       sync.Mutex
	lastSeen time.Time
	count    int64
}

// NewPlugin 创建并返回一个 Stats Plugin 实例。
// Use NewPlugin() if you need a direct reference to the Plugin API (e.g. in tests).
// NewPlugin 创建并返回一个 Stats Plugin 实例。
// 配合 Descriptor(p) 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := stats.NewPlugin()
//	pm.Register(stats.Descriptor(p))
//	engine.Use(p.Middleware())
func NewPlugin() *Plugin {
	return &Plugin{}
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.Register 使用。
func Descriptor(p *Plugin) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "stats",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "用户行为统计插件，记录命令调用次数和用户活跃度",
			Category:    "核心",
			Tags:        []string{"统计", "分析", "监控"},
			HelpText: `统计插件使用说明：
  pm.Register(stats.New())
  engine.Use(statsPlugin.Middleware())
  statsPlugin.TopCommands(10)`,
		},
		Setup: func(setupCtx *plugin.SetupContext) (any, error) {
			setupCtx.Log.Info("Plugin loaded")
			if sb, ok := plugin.Try[storage.Plugin](setupCtx, "storage"); ok {
				p.store = sb.NS("stats")
				p.loadSnapshot()
				setupCtx.Go(func(runCtx stdctx.Context) {
					p.autoSaveWithCtx(runCtx, 5*time.Minute)
				})
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).saveSnapshot()
			return nil
		},
	}
}

// New 创建统计插件描述符（便捷入口，内部创建 Plugin 实例）。
// 若需要持有 Plugin 引用，改用 NewPlugin() + Descriptor()。
func New() *plugin.Descriptor {
	return Descriptor(NewPlugin())
}

// Get 从插件管理器中获取已注册的 Stats 插件实例（类型安全）。
// 需在 pm.Register(New()) 之后调用。
func Get(pm *plugin.Manager) *Plugin {
	v, ok := pm.GetContainer().Get("stats")
	if !ok {
		panic("stats: plugin not registered; call pm.Register(stats.New()) first")
	}
	p, ok := v.(*Plugin)
	if !ok {
		panic("stats: unexpected type in container")
	}
	return p
}

// Middleware 返回自动统计中间件，应挂载到 engine
func (p *Plugin) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			// 统计消息
			p.totalMessages.Add(1)

			// 统计用户（平台无关：优先使用 GetSenderInfo()）
			if info := ctx.GetSenderInfo(); info.ID != "" {
				p.recordUser(info.ID)
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

// statsSnapshot 持久化快照结构
type statsSnapshot struct {
	Commands map[string]int64 `json:"commands"`
	Total    int64            `json:"total"`
}

func (p *Plugin) saveSnapshot() {
	if p.store == nil {
		return
	}
	snap := statsSnapshot{
		Commands: make(map[string]int64),
		Total:    p.totalMessages.Load(),
	}
	p.commandCounts.Range(func(k, v any) bool {
		snap.Commands[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	if err := storage.Set(context.Background(), p.store, "snapshot", snap, 0); err != nil {
		logger.WithError(err).Warn("[Stats] Failed to save snapshot")
	}
}

func (p *Plugin) loadSnapshot() {
	if p.store == nil {
		return
	}
	snap, err := storage.Get[statsSnapshot](context.Background(), p.store, "snapshot")
	if err != nil {
		return
	}
	p.totalMessages.Store(snap.Total)
	for cmd, count := range snap.Commands {
		v, _ := p.commandCounts.LoadOrStore(cmd, new(atomic.Int64))
		v.(*atomic.Int64).Store(count)
	}
	logger.Infof("[Stats] Loaded snapshot: total=%d commands=%d", snap.Total, len(snap.Commands))
}

func (p *Plugin) autoSaveWithCtx(ctx stdctx.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if p.store != nil {
				p.saveSnapshot()
			}
		case <-ctx.Done():
			return
		}
	}
}
