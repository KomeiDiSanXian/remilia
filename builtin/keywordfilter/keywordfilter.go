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
//	pm.RegisterV2(keywordfilter.New(keywordfilter.Config{
//	    Keywords: []string{"违禁词1", "违禁词2"},
//	    OnMatch: func(ctx *eventctx.Context, matched string) error {
//	        return ctx.Reply(platform.TextMessage("消息含有违禁内容，已拦截"))
//	    },
//	}))
//
//	// 作为规则使用
//	kf := ctx.MustGet("keywordfilter").(*keywordfilter.Plugin)
//	engine.On(string(platform.EventKindGroupMessage), kf.Rule()).Handle(handler)
package keywordfilter

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
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
	mu       sync.RWMutex
	keywords []string         // 精确关键词（已规范化大小写）
	patterns []*regexp.Regexp // 编译后的正则
	cfg      Config
}

// New 创建关键词过滤插件描述符
func New(cfg Config) *plugin.Descriptor {
	p := NewPlugin(cfg)
	return Descriptor(p)
}

// NewPlugin 创建 Plugin 实例
func NewPlugin(cfg Config) *Plugin {
	p := &Plugin{cfg: cfg}
	p.setKeywords(cfg.Keywords)
	p.setPatterns(cfg.Patterns)
	return p
}

// Descriptor 从已有 Plugin 创建描述符
func Descriptor(p *Plugin) *plugin.Descriptor {
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
  pm.RegisterV2(keywordfilter.Descriptor(p))
  engine.OnGroupAt(p.Rule()).Handle(handler)
  p.AddKeyword("新敏感词")`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Infof("Loaded with %d keywords, %d patterns", len(p.keywords), len(p.patterns))
			return p, nil
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

// setPatterns 编译正则表达式
func (p *Plugin) setPatterns(patterns []string) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pat := range patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			logger.WithError(err).Warnf("[KeywordFilter] Invalid pattern: %s", pat)
			continue
		}
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
	defer p.mu.Unlock()
	if slices.Contains(p.keywords, keyword) {
		return // 已存在
	}
	p.keywords = append(p.keywords, keyword)
	logger.Debugf("[KeywordFilter] Added keyword: %s", keyword)
}

// RemoveKeyword 动态删除关键词
func (p *Plugin) RemoveKeyword(keyword string) {
	if !p.cfg.CaseSensitive {
		keyword = strings.ToLower(keyword)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	newKws := p.keywords[:0]
	for _, kw := range p.keywords {
		if kw != keyword {
			newKws = append(newKws, kw)
		}
	}
	p.keywords = newKws
}

// AddPattern 动态添加正则表达式
func (p *Plugin) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.patterns = append(p.patterns, re)
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
