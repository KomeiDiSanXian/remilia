// Package knowledgebase 提供本地文档知识库检索工具。
//
// 数据源：本地 Markdown 文档目录（默认 docs/，排除 notes/），
// 启动时增量扫描建索引，分块 embedding 后持久化到 SQLite
// （data/db/knowledgebase.db），查询时按需语义检索。
//
// 对外能力：
//   - kb_search / kb_stats：注册给 AI 插件（ToolProvider），LLM 按需调用
//   - /kb rebuild / /kb status：超管管理命令
package knowledgebase

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Config knowledgebase 插件配置（config.yaml plugins.knowledgebase 节）。
type Config struct {
	// Enabled 是否启用知识库检索（默认 false）。
	Enabled bool `yaml:"enabled"`
	// Markdown 平台支持 MD 时用 Markdown 回复（/kb status、/kb rebuild 等），
	// 否则回退纯文本（默认 true，与 ai 插件一致）。
	Markdown bool `yaml:"markdown"`
	// SourceDir 数据源根目录（相对工作目录，默认 docs）。
	SourceDir string `yaml:"source_dir"`
	// ExcludeDirs 扫描时排除的子目录（默认 ["notes"]：设计笔记多为历史/计划内容）。
	ExcludeDirs []string `yaml:"exclude_dirs"`
	// DBPath 索引持久化路径（默认 data/db/knowledgebase.db）。
	DBPath string `yaml:"db_path"`
	// EmbeddingBaseURL OpenAI 兼容 /embeddings 端点根地址（如 http://host:8080/v1）。
	// 为空时禁用语义检索，仅使用关键词兜底。
	EmbeddingBaseURL string `yaml:"embedding_base_url"`
	// EmbeddingAPIKey 嵌入服务 API Key（自托管服务通常为空）。
	EmbeddingAPIKey string `yaml:"embedding_api_key"`
	// EmbeddingModel 嵌入模型名。
	EmbeddingModel string `yaml:"embedding_model"`
	// ChunkSize 分块目标长度（rune，默认 600）。
	ChunkSize int `yaml:"chunk_size"`
	// ChunkOverlap 相邻分块重叠长度（rune，默认 100）。
	ChunkOverlap int `yaml:"chunk_overlap"`
	// MaxResults kb_search 单次返回条数上限（默认 5）。
	MaxResults int `yaml:"max_results"`
	// ToolDescription kb_search 工具的自定义描述（告诉 LLM 知识库包含什么内容）。
	// 为空时使用通用描述（自动带上数据源目录）。
	ToolDescription string `yaml:"tool_description"`
}

func defaultConfig() Config {
	return Config{
		Enabled:      false, // 默认关闭：opt-in 能力，内容由部署者配置
		Markdown:     true,
		SourceDir:    "",
		ExcludeDirs:  []string{"notes"},
		DBPath:       "data/db/knowledgebase.db",
		ChunkSize:    600,
		ChunkOverlap: 100,
		MaxResults:   5,
	}
}

// Plugin 知识库插件实例（Setup 返回值，同时作为 ai.ToolProvider 注册给 AI 插件）。
type Plugin struct {
	cfg      Config
	log      plugin.Logger
	store    *Store
	embedder ai.Embedder

	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc

	mu      sync.RWMutex
	index   []indexedChunk
	model   string // 生成索引向量的模型名（空 = 关键词兜底模式）
	builtAt time.Time

	building atomic.Bool // 后台重建中（/kb rebuild 异步执行）
	errMu    sync.Mutex
	lastErr  error
}

// New 返回 knowledgebase 插件描述符。
func New() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "knowledgebase",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "本地文档知识库检索（kb_search / kb_stats，超管可重建索引）",
			Category:    "功能",
			Tags:        []string{"knowledgebase", "rag", "docs", "检索", "知识库"},
			HelpText: `知识库插件 — 从本地 Markdown 文档目录（默认 docs/）建立可检索的知识库。

用法：
  /kb rebuild    — 重建/增量更新索引（需 superadmin）
  /kb status     — 查看索引状态（需 superadmin）

AI 工具：
  kb_search      — 检索知识库中最相关的文档片段（LLM 按需调用）
  kb_stats       — 查看知识库索引状态

配置（config.yaml plugins.knowledgebase 节）：
  source_dir: "docs"          # 数据源目录
  exclude_dirs: ["notes"]     # 排除子目录
  embedding_base_url: "http://host:8080/v1"  # OpenAI 兼容嵌入端点（空=仅关键词）
  embedding_model: "Qwen3-Embedding-0.6B-Q8_0.gguf"`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			cfg := loadConfig(ctx)
			p := &Plugin{cfg: cfg, log: ctx.Log}
			// 命令注册（无论是否启用都可查看状态）。
			kbDef := command.NewDef("kb").
				SubCommand(command.NewDef("rebuild").Description("重建知识库索引（需 superadmin）").Build()).
				SubCommand(command.NewDef("status").Description("查看知识库索引状态（需 superadmin）").Build()).
				Build()
			ctx.OnCommandDefWith("", "/kb", kbDef, p.handleKB, eventctx.OnMentionedBotOrNoMentions())

			if !cfg.Enabled {
				ctx.Log.Info("knowledgebase disabled by config (set enabled=true to activate)")
				return p, nil
			}

			// 索引持久化：打开 SQLite（DryRun 阶段不触碰磁盘）。
			if !ctx.DryRun {
				store, err := OpenStore(cfg.DBPath)
				if err != nil {
					return nil, fmt.Errorf("knowledgebase: open store: %w", err)
				}
				p.store = store
			}

			// 嵌入客户端：复用 ai 包实现（熔断器 + 维度校验 + 30s 超时）。
			if cfg.EmbeddingBaseURL != "" {
				if e := ai.NewOpenAIEmbedder(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel); e != nil {
					p.embedder = e
					p.model = e.Model()
					ctx.Log.Infof("knowledgebase semantic search enabled (model %s)", e.Model())
				} else {
					ctx.Log.Warn("knowledgebase: invalid embedding_base_url, keyword-only mode")
				}
			} else {
				ctx.Log.Info("knowledgebase: embedding_base_url not configured, keyword-only mode")
			}

			if ctx.DryRun {
				return p, nil
			}

			p.lifecycleCtx, p.lifecycleCancel = context.WithCancel(context.Background())

			// 加载已有索引到内存；空索引或文件变更时后台增量构建。
			if err := p.loadIndex(); err != nil {
				return nil, fmt.Errorf("knowledgebase: load index: %w", err)
			}
			p.spawn(func() { p.rebuildIfChanged(p.lifecycleCtx) })
			return p, nil
		},
		Teardown: func(tctx *plugin.TeardownContext) error {
			if p, ok := tctx.API.(*Plugin); ok && p.store != nil {
				if p.lifecycleCancel != nil {
					p.lifecycleCancel()
				}
				return p.store.Close()
			}
			return nil
		},
	}
}

func loadConfig(ctx *plugin.SetupContext) Config {
	cfg := defaultConfig()
	if ctx.Config == nil {
		return cfg
	}
	cfg.Enabled = ctx.Config.GetBool("enabled", cfg.Enabled)
	cfg.Markdown = ctx.Config.GetBool("markdown", cfg.Markdown)
	if v := ctx.Config.GetString("source_dir", ""); v != "" {
		cfg.SourceDir = v
	}
	if v := ctx.Config.Get("exclude_dirs"); v != nil {
		if list, ok := v.([]any); ok {
			cfg.ExcludeDirs = cfg.ExcludeDirs[:0]
			for _, item := range list {
				if s, ok := item.(string); ok && s != "" {
					cfg.ExcludeDirs = append(cfg.ExcludeDirs, s)
				}
			}
		}
	}
	if v := ctx.Config.GetString("db_path", ""); v != "" {
		cfg.DBPath = v
	}
	if v := ctx.Config.GetString("embedding_base_url", ""); v != "" {
		cfg.EmbeddingBaseURL = v
	}
	if v := ctx.Config.GetString("embedding_api_key", ""); v != "" {
		cfg.EmbeddingAPIKey = v
	}
	if v := ctx.Config.GetString("embedding_model", ""); v != "" {
		cfg.EmbeddingModel = v
	}
	if v, ok := configInt(ctx, "chunk_size"); ok && v > 0 {
		cfg.ChunkSize = v
	}
	if v, ok := configInt(ctx, "chunk_overlap"); ok && v >= 0 {
		cfg.ChunkOverlap = v
	}
	if v, ok := configInt(ctx, "max_results"); ok && v > 0 {
		cfg.MaxResults = v
	}
	if v := ctx.Config.GetString("tool_description", ""); v != "" {
		cfg.ToolDescription = v
	}
	return cfg
}

func configInt(ctx *plugin.SetupContext, key string) (int, bool) {
	switch v := ctx.Config.Get(key).(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

// isSuperAdmin 判断调用者是否为 superadmin 角色（知识库管理命令权限）。
func isSuperAdmin(ctx *eventctx.Context) bool {
	pm := ctx.GetPermissionManager()
	if pm == nil {
		return false
	}
	for _, role := range pm.GetUserRoles(ctx.GetUserID()) {
		if role == "superadmin" {
			return true
		}
	}
	return false
}

// replyFormatted 按 markdown 配置发送 /kb 子命令回复。
//
// markdown=true 时使用 MarkdownMessage（平台不支持时由发送层自动降级为纯文本）；
// markdown=false 时始终发送纯文本。与 AI 插件子命令回复保持一致，
// 避免 /kb status 等文案（粗体/行内代码）在支持 Markdown 的平台上一律以字面符号展示。
func (p *Plugin) replyFormatted(ctx *eventctx.Context, text string) {
	if p.cfg.Markdown {
		_ = ctx.Reply(platform.MarkdownMessage(text))
		return
	}
	_ = ctx.Reply(platform.TextMessage(text))
}

// log 返回插件日志器（nil 安全）。
func (p *Plugin) logf(format string, args ...any) {
	if p.log != nil {
		p.log.Infof(format, args...)
		return
	}
	logger.Infof(format, args...)
}

// spawn 在插件生命周期上下文上启动后台任务（插件关闭时自动终止）。
func (p *Plugin) spawn(fn func()) {
	if p.lifecycleCtx == nil {
		go fn()
		return
	}
	go func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		select {
		case <-p.lifecycleCtx.Done():
		case <-done:
		}
	}()
}

// setLastErr 记录上次构建错误（nil 表示成功）。
func (p *Plugin) setLastErr(err error) {
	p.errMu.Lock()
	p.lastErr = err
	p.errMu.Unlock()
}

// getLastErr 返回上次构建错误（nil 表示无错误）。
func (p *Plugin) getLastErr() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.lastErr
}
