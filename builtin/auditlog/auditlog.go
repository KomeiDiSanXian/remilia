package auditlog

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

type LogEntryModel struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	Timestamp time.Time `gorm:"index;not null"`
	UserID    string    `gorm:"index;not null"`
	GroupID   string    `gorm:"index"`
	Action    string    `gorm:"index;not null"`
	Content   string    `gorm:"type:text"`
	Meta      string    `gorm:"type:text"`
}

type LogEntry struct {
	ID        int64
	Timestamp time.Time
	UserID    string
	GroupID   string
	Action    string
	Content   string
	Meta      map[string]any
}

type Config struct {
	MaxMemoryEntries int
}

func DefaultConfig() Config {
	return Config{MaxMemoryEntries: 1000}
}

type Plugin struct {
	cfg        Config
	mu         sync.RWMutex
	entries    []LogEntry
	nextID     int64
	Engine     engine.Reader
	storageSvc storage.Client
}

type Option func(*Plugin)

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

func New(cfg ...Config) *plugin.Descriptor {
	c := DefaultConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	p := NewPlugin(c)
	return p.Descriptor()
}

func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "auditlog",
		Version: "1.0.0",
		Deps:    []string{"storage"},
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
			p.Engine = ctx.Info.Coordinator()

			if !ctx.DryRun {
				if svc, ok := plugin.TryService[storage.Client](ctx, "storage"); ok {
					p.storageSvc = svc
					if err := svc.AutoMigrate(&LogEntryModel{}); err != nil {
						ctx.Log.Warnf("Failed to migrate auditlog table: %v", err)
					} else {
						p.loadFromDB()
					}
				}
			}

			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			return nil
		},
	}
}

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

func (p *Plugin) RecordRaw(userID, action string, meta map[string]any) {
	p.append(LogEntry{
		Timestamp: time.Now(),
		UserID:    userID,
		Action:    action,
		Meta:      meta,
	})
}

func (p *Plugin) append(entry LogEntry) {
	p.mu.Lock()
	p.nextID++
	entry.ID = p.nextID
	if len(p.entries) >= p.cfg.MaxMemoryEntries {
		p.entries = p.entries[1:]
	}
	p.entries = append(p.entries, entry)

	// 异步写数据库
	model := p.toModel(entry)
	p.mu.Unlock()

	go p.insertToDB(model)
}

func (p *Plugin) toModel(e LogEntry) LogEntryModel {
	metaStr := ""
	if e.Meta != nil {
		if b, err := json.Marshal(e.Meta); err == nil {
			metaStr = string(b)
		}
	}
	return LogEntryModel{
		ID:        e.ID,
		Timestamp: e.Timestamp,
		UserID:    e.UserID,
		GroupID:   e.GroupID,
		Action:    e.Action,
		Content:   e.Content,
		Meta:      metaStr,
	}
}

func (p *Plugin) insertToDB(model LogEntryModel) {
	if p.storageSvc == nil {
		return
	}
	if err := p.storageSvc.Create(&model); err != nil {
		logger.WithError(err).Warn("[AuditLog] Failed to write entry to DB")
	}
}

func (p *Plugin) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			content := ctx.GetMessageContent()
			if p.isCommand(content) {
				p.Record(ctx, "command", map[string]any{"cmd": content})
			}
			return next(ctx)
		}
	}
}

func (p *Plugin) isCommand(content string) bool {
	if p.Engine == nil {
		return false
	}
	trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
	if trimmed == "" {
		return false
	}
	commands := p.Engine.GetAllCommands()
	for _, cmd := range commands {
		if cmd.Command != "" && strings.HasPrefix(trimmed, cmd.Command) {
			return true
		}
	}
	return false
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

func (p *Plugin) ByUser(userID string, n int) []LogEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []LogEntry
	for i := len(p.entries) - 1; i >= 0 && (n <= 0 || len(result) < n); i-- {
		if p.entries[i].UserID == userID {
			result = append(result, p.entries[i])
		}
	}
	slices.Reverse(result)
	return result
}

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

func (p *Plugin) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}

// ListTools 返回可供 AI 调用的工具集。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "audit_log_recent",
			Description: "查询最近的审计日志条目。返回最近 N 条操作记录的用户、操作类型和内容。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"count": {Type: "string", Description: "返回条数，默认 10，最大 50"},
				},
			},
			Execute: func(_ context.Context, args map[string]any) (string, error) {
				n := 10
				if v, ok := args["count"]; ok {
					fmt.Sscanf(fmt.Sprint(v), "%d", &n)
				}
				if n > 50 {
					n = 50
				}
				entries := p.Recent(n)
				if len(entries) == 0 {
					return "暂无审计日志", nil
				}
				var b strings.Builder
				b.WriteString("**最近操作：**\n")
				for _, e := range entries {
					b.WriteString(fmt.Sprintf("- [%s] %s 执行了 `%s`", e.Timestamp.Format("15:04"), e.UserID, e.Action))
					if e.Content != "" {
						b.WriteString(fmt.Sprintf("：%s", truncateText(e.Content, 60)))
					}
					b.WriteString("\n")
				}
				return b.String(), nil
			},
		},
		{
			Name:        "audit_log_search",
			Description: "搜索审计日志中指定用户的操作记录。返回最近 N 条该用户的操作。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"user_id": {Type: "string", Description: "用户 ID"},
					"count":   {Type: "string", Description: "返回条数，默认 10，最大 50"},
				},
				Required: []string{"user_id"},
			},
			Execute: func(_ context.Context, args map[string]any) (string, error) {
				userID, _ := args["user_id"].(string)
				if userID == "" {
					return "请提供 user_id", nil
				}
				n := 10
				if v, ok := args["count"]; ok {
					fmt.Sscanf(fmt.Sprint(v), "%d", &n)
				}
				if n > 50 {
					n = 50
				}
				entries := p.ByUser(userID, n)
				if len(entries) == 0 {
					return fmt.Sprintf("用户 %s 暂无操作记录", userID), nil
				}
				var b strings.Builder
				b.WriteString(fmt.Sprintf("**用户 %s 最近操作：**\n", userID))
				for _, e := range entries {
					b.WriteString(fmt.Sprintf("- [%s] `%s`", e.Timestamp.Format("15:04"), e.Action))
					if e.Content != "" {
						b.WriteString(fmt.Sprintf("：%s", truncateText(e.Content, 60)))
					}
					b.WriteString("\n")
				}
				return b.String(), nil
			},
		},
	}
}

// truncateText 截断文本到指定长度。
func truncateText(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func (p *Plugin) loadFromDB() {
	if p.storageSvc == nil {
		return
	}
	// 尝试获取具体 storage.Plugin 类型以使用链式查询
	type pluginClient interface {
		Order(value any) *storage.Plugin
		Limit(limit int) *storage.Plugin
		Find(dest any, conds ...any) error
	}
	pc, ok2 := p.storageSvc.(pluginClient)
	if ok2 {
		var models []LogEntryModel
		if err := pc.Order("id DESC").Limit(p.cfg.MaxMemoryEntries).Find(&models); err != nil {
			return
		}
		p.loadModels(models)
		return
	}
	var models []LogEntryModel
	if err := p.storageSvc.Find(&models); err != nil {
		return
	}
	p.loadModels(models)
}

func (p *Plugin) loadModels(models []LogEntryModel) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = make([]LogEntry, 0, len(models))
	for i := len(models) - 1; i >= 0; i-- {
		m := models[i]
		entry := LogEntry{
			ID:        m.ID,
			Timestamp: m.Timestamp,
			UserID:    m.UserID,
			GroupID:   m.GroupID,
			Action:    m.Action,
			Content:   m.Content,
		}
		if m.Meta != "" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(m.Meta), &meta); err == nil {
				entry.Meta = meta
			}
		}
		p.entries = append(p.entries, entry)
	}
	if len(p.entries) > 0 {
		p.nextID = p.entries[len(p.entries)-1].ID
	}
	logger.Infof("[AuditLog] Loaded %d entries from DB", len(p.entries))
}
