// Package auditlog 提供操作审计日志插件。
//
// 功能：
//   - 自动记录命令调用（通过中间件）
//   - 记录管理操作（权限变更、插件操作等）
//   - 可选持久化到 storage 插件
//   - 提供查询接口（按用户/命令/时间查询）
//
// 使用示例:
//
//	pm.Register(auditlog.New())
//	// 挂载中间件：
//	engine.Use(auditlogPlugin.Middleware())
//	// 手动记录：
//	al := ctx.MustGet("auditlog").(*auditlog.Plugin)
//	al.Record(ctx, "perm.grant", map[string]any{"target": userID, "role": "admin"})
package auditlog

import (
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// LogEntry 审计日志条目
type LogEntry struct {
	ID        int64          `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	UserID    string         `json:"user_id"`
	GroupID   string         `json:"group_id,omitempty"`
	Action    string         `json:"action"`
	Content   string         `json:"content,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

// Config 审计日志配置
type Config struct {
	// MaxMemoryEntries 内存中保留的最大条目数（环形缓冲，默认 1000）
	MaxMemoryEntries int
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{MaxMemoryEntries: 1000}
}

// Plugin 审计日志插件 API
type Plugin struct {
	cfg      Config
	mu       sync.RWMutex
	entries  []LogEntry
	nextID   int64
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
		cfg:     cfg,
		entries: make([]LogEntry, 0, cfg.MaxMemoryEntries),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// New 创建审计日志插件描述符
func New(cfg ...Config) *plugin.Descriptor {
	c := DefaultConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	p := NewPlugin(c)
	return p.Descriptor()
}

// Descriptor 从已有 Plugin 创建描述符
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "auditlog",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "操作审计日志插件，记录命令调用和管理操作",
			Category:    "安全",
			Tags:        []string{"安全", "审计", "日志"},
			HelpText: `审计日志插件使用说明：
  al := plugin.Require[auditlog.Plugin](ctx, "auditlog")
  al.Record(ctx, "perm.grant", ...)
  entries := al.Recent(50)`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			p.loadFromFile()
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).flushToFile()
			return nil
		},
	}
}

// Record 记录一条审计日志
func (p *Plugin) Record(ctx *eventctx.Context, action string, meta ...map[string]any) {
	userID := ctx.GetSenderInfo().ID
	content := ctx.GetMessageContent()

	var m map[string]any
	if len(meta) > 0 {
		m = meta[0]
	}

	p.append(LogEntry{
		Timestamp: time.Now(),
		UserID:    userID,
		Action:    action,
		Content:   content,
		Meta:      m,
	})
}

// RecordRaw 记录一条原始审计日志（不依赖 Context）
func (p *Plugin) RecordRaw(userID, action string, meta map[string]any) {
	p.append(LogEntry{
		Timestamp: time.Now(),
		UserID:    userID,
		Action:    action,
		Meta:      meta,
	})
}

// append 追加日志条目（环形缓冲）
func (p *Plugin) append(entry LogEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	entry.ID = p.nextID
	if len(p.entries) >= p.cfg.MaxMemoryEntries {
		p.entries = p.entries[1:] // 丢弃最旧的
	}
	p.entries = append(p.entries, entry)

	// 异步持久化
	if p.dataFile != "" {
		go p.flushToFile()
	}
}

// Middleware 返回自动记录命令调用的中间件
func (p *Plugin) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			content := ctx.GetMessageContent()
			if len(content) > 0 && content[0] == '/' {
				p.Record(ctx, "command", map[string]any{"cmd": content})
			}
			return next(ctx)
		}
	}
}
func (p *Plugin) Recent(n int) []LogEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if n <= 0 || n >= len(p.entries) {
		out := make([]LogEntry, len(p.entries))
		copy(out, p.entries)
		return out
	}
	out := make([]LogEntry, n)
	copy(out, p.entries[len(p.entries)-n:])
	return out
}

// ByUser 返回指定用户最近 n 条日志
func (p *Plugin) ByUser(userID string, n int) []LogEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []LogEntry
	for i := len(p.entries) - 1; i >= 0 && (n <= 0 || len(result) < n); i-- {
		if p.entries[i].UserID == userID {
			result = append(result, p.entries[i])
		}
	}
	// reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// ByAction 返回指定操作类型最近 n 条日志
func (p *Plugin) ByAction(action string, n int) []LogEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []LogEntry
	for i := len(p.entries) - 1; i >= 0 && (n <= 0 || len(result) < n); i-- {
		if p.entries[i].Action == action {
			result = append(result, p.entries[i])
		}
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// Count 返回总日志条目数
func (p *Plugin) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}

type storageData struct {
	Entries []LogEntry `json:"entries"`
	NextID  int64      `json:"next_id"`
}

func (p *Plugin) flushToFile() {
	if p.dataFile == "" {
		return
	}
	p.mu.RLock()
	d := storageData{
		Entries: make([]LogEntry, len(p.entries)),
		NextID:  p.nextID,
	}
	copy(d.Entries, p.entries)
	p.mu.RUnlock()

	if err := jsonfile.Write(p.dataFile, d); err != nil {
		logger.WithError(err).Warn("[AuditLog] Failed to flush to file")
	}
}

func (p *Plugin) loadFromFile() {
	if p.dataFile == "" {
		return
	}
	d, err := jsonfile.Read[storageData](p.dataFile)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(d.Entries) > p.cfg.MaxMemoryEntries {
		d.Entries = d.Entries[len(d.Entries)-p.cfg.MaxMemoryEntries:]
	}
	p.entries = d.Entries
	p.nextID = d.NextID
	logger.Infof("[AuditLog] Loaded %d entries from file", len(p.entries))
}
