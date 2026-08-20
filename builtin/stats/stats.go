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
//	statsSvc := ctx.Service[*stats.Plugin]("stats")
//	top := sp.TopCommands(10)
package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
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
	commandKeys   atomic.Int64
	userStats     sync.Map // userID -> *userEntry
	totalMessages atomic.Int64
	kvPath        string // LevelDB 数据库路径（空字符串=纯内存）
	store         *kv.DB
}

// commandCounts 的容量与键长上限。
//
// Middleware 会把"首字符非字母数字"的任意 token 当成命令记录下来，
// 而这完全由用户消息内容决定（标点、emoji 开头都算），该 map 既无淘汰、
// 又每 5 分钟整体快照落盘。不加上限时，用户只要持续发送互不相同的
// 标点前缀 token，就能让内存和磁盘无限增长。
const (
	maxTrackedCommands = 1000
	maxCommandKeyLen   = 64
)

type userEntry struct {
	mu       sync.Mutex
	lastSeen time.Time
	count    int64
}

// Option 配置选项
type Option func(*Plugin)

// WithStore 设置 LevelDB 存储路径。空字符串表示纯内存模式。
func WithStore(path string) Option {
	return func(p *Plugin) { p.kvPath = path }
}

// NewPlugin 创建并返回一个 Stats Plugin 实例。
// 配合 p.Descriptor() 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := stats.NewPlugin()
//	pm.Register(p.Descriptor())
//	engine.Use(p.Middleware())
func NewPlugin(opts ...Option) *Plugin {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.Register 使用。
func (p *Plugin) Descriptor() *plugin.Descriptor {
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
			if !setupCtx.DryRun && p.kvPath != "" {
				var err error
				p.store, err = kv.Open(p.kvPath)
				if err != nil {
					return nil, err
				}
				p.loadSnapshot()
				setupCtx.Spawn(func(runCtx context.Context) { p.autoSaveWithCtx(runCtx, 5*time.Minute) })
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).saveSnapshot()
			if ctx.API.(*Plugin).store != nil {
				return ctx.API.(*Plugin).store.Close()
			}
			return nil
		},
	}
}

// New 创建统计插件描述符（便捷入口）。
func New(opts ...Option) *plugin.Descriptor {
	return NewPlugin(opts...).Descriptor()
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

			// 统计命令（检测首词是否以非字母/数字开头——即命令前缀）
			content := strings.TrimSpace(ctx.GetMessageContent())
			fields := strings.Fields(content)
			if len(fields) > 0 {
				first := []rune(fields[0])[0]
				if !unicode.IsLetter(first) && !unicode.IsDigit(first) {
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
	if cmd == "" || len(cmd) > maxCommandKeyLen {
		return
	}
	// 已存在的键始终照常计数，不受上限影响
	if v, ok := p.commandCounts.Load(cmd); ok {
		v.(*atomic.Int64).Add(1)
		return
	}
	// 新键受总量上限保护，防止用户构造任意 token 撑爆 map 与快照
	if p.commandKeys.Load() >= maxTrackedCommands {
		return
	}
	v, loaded := p.commandCounts.LoadOrStore(cmd, new(atomic.Int64))
	if !loaded {
		p.commandKeys.Add(1)
	}
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

// ListSkills 返回可供 AI 调用的技能集。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "stats_analyst",
			Description: "使用分析：查询命令调用排行、单个命令调用次数、总消息数",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"query": {Type: "string", Description: "统计相关的问题，如'最常用的命令是什么'或'/ping 被调用了多少次'"},
				},
				Required: []string{"query"},
			},
			Prompt: "你是一个机器人使用分析助手。用户会问你关于使用情况的问题，如哪些命令最受欢迎、某个命令的调用次数、总处理消息数。使用统计工具提供清晰的数据分析。",
			Tools:  p.ListTools(),
		},
	}
}

// ListTools 返回可供 AI 调用的工具集。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "stats_top_commands",
			Categories:  []string{"admin"},
			Description: "返回调用次数最多的 N 个命令及其调用次数。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"count": {Type: "string", Description: "返回条数，默认 10"},
				},
			},
			Execute: func(_ context.Context, args map[string]any) (string, error) {
				n := 10
				if v, ok := args["count"]; ok {
					fmt.Sscanf(fmt.Sprint(v), "%d", &n)
				}
				stats := p.TopCommands(n)
				if len(stats) == 0 {
					return "暂无命令统计数据", nil
				}
				var b strings.Builder
				b.WriteString("**命令调用排行：**\n")
				for i, s := range stats {
					b.WriteString(fmt.Sprintf("%d. `%s` — %d 次\n", i+1, s.Command, s.Count))
				}
				return b.String(), nil
			},
		},
		{
			Name:        "stats_command_count",
			Categories:  []string{"admin"},
			Description: "查询指定命令的累计调用次数。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"command": {Type: "string", Description: "命令名称，如 /ping"},
				},
				Required: []string{"command"},
			},
			Execute: func(_ context.Context, args map[string]any) (string, error) {
				cmd, _ := args["command"].(string)
				if cmd == "" {
					return "请提供命令名称", nil
				}
				count := p.CommandCount(cmd)
				return fmt.Sprintf("命令 `%s` 已被调用 %d 次", cmd, count), nil
			},
		},
		{
			Name:        "stats_total_messages",
			Categories:  []string{"admin"},
			Description: "查询 bot 处理的总消息数。",
			Parameters: ai.ToolParamSchema{
				Type:       "object",
				Properties: map[string]ai.ToolParamSchema{},
			},
			Execute: func(_ context.Context, args map[string]any) (string, error) {
				return fmt.Sprintf("bot 共处理了 %d 条消息", p.TotalMessages()), nil
			},
		},
	}
}

// Reset 重置所有统计数据
func (p *Plugin) Reset() {
	p.commandCounts = sync.Map{}
	p.commandKeys.Store(0)
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
	bytes, err := json.Marshal(snap)
	if err != nil {
		logger.WithError(err).Warn("[Stats] Failed to marshal snapshot")
		return
	}
	if err := p.store.Set([]byte("state"), bytes); err != nil {
		logger.WithError(err).Warn("[Stats] Failed to save snapshot")
	}
}

func (p *Plugin) loadSnapshot() {
	if p.store == nil {
		return
	}
	bytes, err := p.store.Get([]byte("state"))
	if err != nil {
		return
	}
	var snap statsSnapshot
	if err := json.Unmarshal(bytes, &snap); err != nil {
		return
	}
	p.totalMessages.Store(snap.Total)
	for cmd, count := range snap.Commands {
		v, loaded := p.commandCounts.LoadOrStore(cmd, new(atomic.Int64))
		if !loaded {
			p.commandKeys.Add(1)
		}
		v.(*atomic.Int64).Store(count)
	}
	logger.Infof("[Stats] Loaded snapshot: total=%d commands=%d", snap.Total, len(snap.Commands))
}

func (p *Plugin) autoSaveWithCtx(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.saveSnapshot()
		case <-ctx.Done():
			return
		}
	}
}
