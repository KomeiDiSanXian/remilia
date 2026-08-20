// Package keywordfilter 提供关键词过滤插件。
//
// 功能：
//   - 维护敏感词/违禁词列表
//   - 支持精确匹配和正则匹配两种模式
//   - 使用 Aho-Corasick 风格的多关键词高效匹配（基于 strings.Contains 批量检测）
//   - 可配置违规处理回调（拦截/警告/记录）
//   - 动态增减关键词，无需重启
//
// 使用示例:
//
//	pm.Register(keywordfilter.New(keywordfilter.Config{
//	    Keywords: []string{"违禁词1", "违禁词2"},
//	    OnMatch: func(ctx *eventctx.Context, matched string) error {
//	        return ctx.Reply(platform.TextMessage("消息含有违禁内容，已拦截"))
//	    },
//	}))
//
//	// 作为规则使用
//	kfSvc := ctx.Service[*keywordfilter.Plugin]("keywordfilter")
//	engine.On(string(platform.EventKindGroupMessage), kf.Rule()).Handle(handler)
package keywordfilter

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// MatchHandler 关键词匹配处理函数
type MatchHandler func(ctx *eventctx.Context, matched string) error

// Config 关键词过滤配置
type Config struct {
	// Keywords 精确关键词列表（包含匹配，不区分大小写）
	Keywords []string
	// Patterns 正则表达式列表
	Patterns []string
	// CaseSensitive 是否区分大小写（默认 false）
	CaseSensitive bool
	// OnMatch 匹配到关键词时的回调（返回非 nil 错误则中断处理链）
	// 如果为 nil，匹配时只记录日志
	OnMatch MatchHandler
}

// Plugin 关键词过滤插件 API
type Plugin struct {
	mu          sync.RWMutex
	keywords    []string         // 精确关键词（已规范化大小写）
	rawPatterns []string         // 原始正则表达式字符串（供持久化用）
	patterns    []*regexp.Regexp // 编译后的正则
	cfg         Config
	kvPath      string
	store       *kv.DB
}

type Option func(*Plugin)

func WithStore(path string) Option {
	return func(p *Plugin) { p.kvPath = path }
}

// New 创建关键词过滤插件描述符
func New(cfg Config, opts ...Option) *plugin.Descriptor {
	return NewPlugin(cfg, opts...).Descriptor()
}

// NewPlugin 创建 Plugin 实例
func NewPlugin(cfg Config, opts ...Option) *Plugin {
	p := &Plugin{cfg: cfg}
	for _, o := range opts {
		o(p)
	}
	p.setKeywords(cfg.Keywords)
	p.setPatterns(cfg.Patterns)
	return p
}

// Descriptor 从已有 Plugin 创建描述符
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "keywordfilter",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "关键词过滤插件，屏蔽违禁/敏感内容",
			Category:    "安全",
			Tags:        []string{"安全", "过滤", "关键词"},
			HelpText: `关键词过滤插件使用说明：
  p := keywordfilter.NewPlugin(keywordfilter.Config{Keywords: []string{"违禁词"}})
  pm.Register(p.Descriptor())
  engine.OnGroupAt(p.Rule()).Handle(handler)
  p.AddKeyword("新敏感词")`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if ctx.Config != nil {
				if kw := ctx.Config.GetStringSlice("keywords", nil); len(kw) > 0 {
					p.setKeywords(kw)
				}
				if pt := ctx.Config.GetStringSlice("patterns", nil); len(pt) > 0 {
					p.setPatterns(pt)
				}
			}
			ctx.Log.Infof("Loaded with %d keywords, %d patterns", len(p.keywords), len(p.patterns))
			if !ctx.DryRun && p.kvPath != "" {
				db, err := kv.Open(p.kvPath)
				if err != nil {
					return nil, err
				}
				p.store = db
				p.load(ctx)
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			p.save()
			if p.store != nil {
				p.store.Close()
			}
			return nil
		},
	}
}

// setKeywords 设置关键词（内部，规范化大小写）
func (p *Plugin) setKeywords(keywords []string) {
	normalized := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if !p.cfg.CaseSensitive {
			kw = strings.ToLower(kw)
		}
		normalized = append(normalized, kw)
	}
	p.keywords = normalized
}

// setPatterns 编译正则表达式并记录原始字符串（供持久化用）
func (p *Plugin) setPatterns(patterns []string) {
	p.rawPatterns = make([]string, 0, len(patterns))
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pat := range patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			logger.WithError(err).Warnf("[KeywordFilter] Invalid pattern: %s", pat)
			continue
		}
		p.rawPatterns = append(p.rawPatterns, pat)
		compiled = append(compiled, re)
	}
	p.patterns = compiled
}

// AddKeyword 动态添加关键词
func (p *Plugin) AddKeyword(keyword string) {
	if keyword == "" {
		return
	}
	if !p.cfg.CaseSensitive {
		keyword = strings.ToLower(keyword)
	}
	p.mu.Lock()
	if slices.Contains(p.keywords, keyword) {
		p.mu.Unlock()
		return // 已存在
	}
	p.keywords = append(p.keywords, keyword)
	p.mu.Unlock()
	p.save()
	logger.Debugf("[KeywordFilter] Added keyword: %s", keyword)
}

// RemoveKeyword 动态删除关键词
func (p *Plugin) RemoveKeyword(keyword string) {
	if !p.cfg.CaseSensitive {
		keyword = strings.ToLower(keyword)
	}
	p.mu.Lock()
	newKws := p.keywords[:0]
	for _, kw := range p.keywords {
		if kw != keyword {
			newKws = append(newKws, kw)
		}
	}
	p.keywords = newKws
	p.mu.Unlock()
	p.save()
}

// AddPattern 动态添加正则表达式
func (p *Plugin) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.rawPatterns = append(p.rawPatterns, pattern)
	p.patterns = append(p.patterns, re)
	p.mu.Unlock()
	p.save()
	return nil
}

// Check 检查文本是否包含违禁关键词，返回第一个匹配的词（无匹配返回空字符串）
func (p *Plugin) Check(text string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	checkText := text
	if !p.cfg.CaseSensitive {
		checkText = strings.ToLower(text)
	}

	for _, kw := range p.keywords {
		if strings.Contains(checkText, kw) {
			return kw
		}
	}

	for _, re := range p.patterns {
		if m := re.FindString(text); m != "" {
			return m
		}
	}

	return ""
}

// ListTools 返回可供 AI 调用的工具集。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "keyword_check",
			Categories:  []string{"admin"},
			Description: "检查文本是否包含违禁/敏感关键词。返回匹配到的第一个关键词，无匹配则返回空。",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"text": {Type: "string", Description: "要检查的文本内容"},
				},
				Required: []string{"text"},
			},
			Execute: func(_ context.Context, args map[string]any) (string, error) {
				text, _ := args["text"].(string)
				if text == "" {
					return "请提供要检查的文本", nil
				}
				if matched := p.Check(text); matched != "" {
					return fmt.Sprintf("⚠️ 文本包含敏感内容「%s」", matched), nil
				}
				return "✅ 文本未发现敏感内容", nil
			},
		},
	}
}

// Rule 返回可用于 engine.On() 的过滤规则（匹配到关键词则拦截，返回 false）
func (p *Plugin) Rule() eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		content := ctx.GetMessageContent()
		matched := p.Check(content)
		if matched == "" {
			return true // 无匹配，放行
		}

		logger.Debugf("[KeywordFilter] Blocked message containing: %s", matched)
		if p.cfg.OnMatch != nil {
			if err := p.cfg.OnMatch(ctx, matched); err != nil {
				logger.WithError(err).Warn("[KeywordFilter] OnMatch handler error")
			}
		}
		return false // 拦截
	}
}

// Middleware 返回可用于 engine.OnXxx().Use() 的过滤中间件
// 与 Rule() 不同，Middleware 在匹配后仍可继续处理链（由 OnMatch 决定）
func (p *Plugin) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			content := ctx.GetMessageContent()
			matched := p.Check(content)
			if matched == "" {
				return next(ctx)
			}

			logger.Debugf("[KeywordFilter] Message contains keyword: %s", matched)
			if p.cfg.OnMatch != nil {
				return p.cfg.OnMatch(ctx, matched)
			}
			return nil // 默认静默拦截
		}
	}
}

// KeywordCount 返回当前关键词数量
func (p *Plugin) KeywordCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.keywords)
}

// PatternCount 返回当前正则表达式数量
func (p *Plugin) PatternCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.patterns)
}

// ─── 持久化 ────────────────────────────────────────────────────────────────

// kfSnapshot 是持久化的 JSON 格式
type kfSnapshot struct {
	// Keywords 保存规范化后的关键词列表（config 初始 + 动态添加 - 动态删除）
	Keywords []string `json:"keywords"`
	// Patterns 保存原始正则表达式字符串列表
	Patterns []string `json:"patterns"`
}

// save 将当前关键词和正则列表持久化到 LevelDB（异步调用）。
// 若 store 为空则静默跳过。
func (p *Plugin) save() {
	if p.store == nil {
		return
	}
	p.mu.RLock()
	kws := make([]string, len(p.keywords))
	copy(kws, p.keywords)
	pats := make([]string, len(p.rawPatterns))
	copy(pats, p.rawPatterns)
	p.mu.RUnlock()

	snap := kfSnapshot{Keywords: kws, Patterns: pats}
	bytes, err := json.Marshal(snap)
	if err != nil {
		logger.WithError(err).Warn("[KeywordFilter] Failed to marshal state")
		return
	}
	if err := p.store.Set([]byte("state"), bytes); err != nil {
		logger.WithError(err).Warn("[KeywordFilter] Failed to save state")
	}
}

// load 从 LevelDB 加载关键词和正则列表，替换当前内存状态（Setup 时调用）。
// 若键不存在则静默跳过（保持 config 初始值）。
// 若数据存在，其内容将作为权威状态替换 Config.Keywords/Patterns。
func (p *Plugin) load(ctx *plugin.SetupContext) {
	if p.store == nil {
		return
	}
	bytes, err := p.store.Get([]byte("state"))
	if err != nil {
		return
	}
	var snap kfSnapshot
	if err := json.Unmarshal(bytes, &snap); err != nil {
		ctx.Log.Warnf("[KeywordFilter] Failed to unmarshal state: %v", err)
		return
	}

	compiled := make([]*regexp.Regexp, 0, len(snap.Patterns))
	validPats := snap.Patterns[:0]
	for _, pat := range snap.Patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			ctx.Log.Warnf("[KeywordFilter] Skipping invalid pattern from file: %s: %v", pat, err)
			continue
		}
		compiled = append(compiled, re)
		validPats = append(validPats, pat)
	}

	p.mu.Lock()
	p.keywords = snap.Keywords
	p.rawPatterns = validPats
	p.patterns = compiled
	p.mu.Unlock()
	ctx.Log.Infof("[KeywordFilter] Loaded %d keywords, %d patterns from store",
		len(snap.Keywords), len(validPats))
}
