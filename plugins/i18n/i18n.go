// Package i18n 提供国际化/本地化插件。
//
// 支持从 YAML 文件加载语言包，并与 config.Watcher 联动热更新。
//
// 使用示例:
//
//	pm.RegisterV2(i18n.New(i18n.Config{
//	    DefaultLocale: "zh-CN",
//	    LocaleDir:     "locales/",
//	}))
//	// Handler 中：
//	t := ctx.MustGet("i18n").(*i18n.Plugin)
//	msg := t.T(ctx, "welcome", map[string]any{"name": "Alice"})
//
// locale 文件格式（locales/zh-CN.yaml）：
//
//	welcome: "欢迎, {{.name}}！"
//	help.title: "帮助菜单"
package i18n

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	lru "github.com/hashicorp/golang-lru/v2"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"gopkg.in/yaml.v3"
)

// localeKey 存储用户语言偏好的 Context key
const localeKey = "_i18n_locale"

// templateCacheSize 模板 LRU 缓存容量（按 locale+key 独立缓存）
const templateCacheSize = 2048

// Config i18n 插件配置
type Config struct {
	// DefaultLocale 默认语言，如 "zh-CN"
	DefaultLocale string
	// LocaleDir 语言文件目录（YAML），每个文件名即为 locale ID，如 "zh-CN.yaml"
	LocaleDir string
	// Fallback 当翻译不存在时的回退语言（默认同 DefaultLocale）
	Fallback string
}

// bundle 一个语言包（key → 翻译文本）
type bundle struct {
	locale string
	msgs   map[string]string
}

// Plugin i18n 插件 API
type Plugin struct {
	cfg     Config
	bundles sync.Map // locale -> *bundle
	// tmplCache 预编译模板缓存，key 为 "locale\x00msgKey"，避免重复 Parse 开销
	tmplCache *lru.Cache[string, *template.Template]
}

// NewPlugin 创建并返回一个已初始化的 i18n Plugin 实例。
// 配合 Descriptor(p) 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := i18n.NewPlugin(i18n.Config{DefaultLocale: "zh-CN"})
//	pm.RegisterV2(i18n.Descriptor(p))
//	p.LoadBytes("zh-CN", data)
func NewPlugin(cfg Config) *Plugin {
	if cfg.DefaultLocale == "" {
		cfg.DefaultLocale = "zh-CN"
	}
	if cfg.Fallback == "" {
		cfg.Fallback = cfg.DefaultLocale
	}
	cache, err := lru.New[string, *template.Template](templateCacheSize)
	if err != nil {
		// lru.New 仅在 size <= 0 时返回错误；templateCacheSize 为正数常量，此处实际永不触发。
		panic(fmt.Sprintf("i18n: failed to create template cache: %v", err))
	}
	return &Plugin{cfg: cfg, tmplCache: cache}
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.RegisterV2 使用。
func Descriptor(p *Plugin) *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:    "i18n",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "国际化/本地化插件，支持多语言文本和热更新",
			Category:    "核心",
			Tags:        []string{"i18n", "国际化", "多语言"},
			HelpText: `i18n 插件使用说明：
  p := i18n.NewPlugin(i18n.Config{DefaultLocale: "zh-CN"})
  pm.RegisterV2(i18n.Descriptor(p))
  p.T(ctx, "key")`,
		},
		Advanced: &plugin.PluginAdvanced{
			Reload: func(setupCtx *plugin.SetupContext) error {
				if p.cfg.LocaleDir != "" {
					return p.loadDir(p.cfg.LocaleDir)
				}
				return nil
			},
		},
		Setup: func(setupCtx *plugin.SetupContext) (any, error) {
			setupCtx.Log.Infof("Loading locales from '%s', default=%s", p.cfg.LocaleDir, p.cfg.DefaultLocale)
			if p.cfg.LocaleDir != "" {
				if err := p.loadDir(p.cfg.LocaleDir); err != nil {
					setupCtx.Log.Error("Failed to load locale dir, continuing with empty bundles", err)
				}
			}
			return p, nil
		},
	}
}

// New 创建 i18n 插件描述符（便捷入口，内部创建 Plugin 实例）。
// 若需要持有 Plugin 引用，改用 NewPlugin(cfg) + Descriptor()。
func New(cfg Config) *plugin.PluginDescriptor {
	return Descriptor(NewPlugin(cfg))
}

// Get 从插件管理器中获取已注册的 i18n 插件实例（类型安全）。
// 需在 pm.RegisterV2(New(cfg)) 之后调用。
func Get(pm *plugin.Manager) *Plugin {
	v, ok := pm.GetContainer().Get("i18n")
	if !ok {
		panic("i18n: plugin not registered; call pm.RegisterV2(i18n.New(cfg)) first")
	}
	p, ok := v.(*Plugin)
	if !ok {
		panic("i18n: unexpected type in container")
	}
	return p
}

// loadDir 加载目录中所有 *.yaml 语言文件
func (p *Plugin) loadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("i18n: read dir %s: %w", dir, err)
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		locale := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
		path := filepath.Join(dir, name)
		if err := p.LoadFile(locale, path); err != nil {
			logger.WithError(err).Warnf("[i18n] Failed to load locale %s from %s", locale, path)
		} else {
			count++
		}
	}
	logger.Infof("[i18n] Loaded %d locale file(s)", count)
	return nil
}

// LoadFile 从 YAML 文件加载指定 locale 的翻译
func (p *Plugin) LoadFile(locale, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("i18n: read %s: %w", path, err)
	}
	return p.LoadBytes(locale, data)
}

// LoadBytes 从 YAML 字节加载 locale 翻译
// 加载新语言包时清除该 locale 下的模板缓存，确保缓存一致性
func (p *Plugin) LoadBytes(locale string, data []byte) error {
	var msgs map[string]string
	if err := yaml.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("i18n: parse yaml for %s: %w", locale, err)
	}
	p.bundles.Store(locale, &bundle{locale: locale, msgs: msgs})
	// 清除该 locale 的模板缓存（语言包变更后旧缓存失效）
	p.evictLocaleCache(locale)
	logger.Debugf("[i18n] Loaded locale %s (%d messages)", locale, len(msgs))
	return nil
}

// evictLocaleCache 清除指定 locale 对应的所有模板缓存
func (p *Plugin) evictLocaleCache(locale string) {
	if p.tmplCache == nil {
		return
	}
	prefix := locale + "\x00"
	for _, k := range p.tmplCache.Keys() {
		if strings.HasPrefix(k, prefix) {
			p.tmplCache.Remove(k)
		}
	}
}

// SetLocale 在 Context 中设置用户语言偏好（仅对当次请求有效）
func (p *Plugin) SetLocale(ctx *eventctx.Context, locale string) {
	ctx.Set(localeKey, locale)
}

// GetLocale 获取当前请求的语言
func (p *Plugin) GetLocale(ctx *eventctx.Context) string {
	if v, ok := ctx.Get(localeKey); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return p.cfg.DefaultLocale
}

// T 翻译 key，args 为可选的模板变量 map[string]any
func (p *Plugin) T(ctx *eventctx.Context, key string, args ...map[string]any) string {
	locale := p.GetLocale(ctx)
	text := p.lookup(locale, key)
	if text == "" {
		return key
	}
	if len(args) == 0 {
		return text
	}
	return p.render(locale, key, text, args[0])
}

// Tn 复数形式翻译。
// locale 文件中用 ".one" / ".other"（以及 ".zero"、".two"、".few"、".many"）后缀区分复数形式：
//
//	items.one:   "{{.Count}} 个项目"
//	items.other: "{{.Count}} 个项目"
//	items.zero:  "没有项目"
//
// 规则：
//   - count == 0 → key.zero（若不存在则 fallback 到 key.other）
//   - count == 1 → key.one（若不存在则 fallback 到 key.other）
//   - count == 2 → key.two（若不存在则 fallback 到 key.other）
//   - count >= 3 → key.other（英语等大多数语言）
//
// 若 args 为 nil，则自动注入 {"Count": count}。
func (p *Plugin) Tn(ctx *eventctx.Context, key string, count int, args map[string]any) string {
	locale := p.GetLocale(ctx)
	// 选择复数后缀候选列表（按优先级从高到低）
	suffixes := pluralSuffixes(count)
	var (
		text    string
		usedKey string
	)
	for _, suffix := range suffixes {
		candidate := key + suffix
		t := p.lookup(locale, candidate)
		if t != "" {
			text = t
			usedKey = candidate
			break
		}
	}
	// 若所有带后缀的 key 均不存在，尝试不带后缀的原 key
	if text == "" {
		text = p.lookup(locale, key)
		usedKey = key
	}
	if text == "" {
		return key
	}
	// 合并参数，自动注入 Count
	merged := map[string]any{"Count": count}
	maps.Copy(merged, args)
	return p.render(locale, usedKey, text, merged)
}

// pluralSuffixes 根据 count 返回复数后缀优先级列表
func pluralSuffixes(count int) []string {
	switch {
	case count == 0:
		return []string{".zero", ".other"}
	case count == 1:
		return []string{".one", ".other"}
	case count == 2:
		return []string{".two", ".other"}
	case count >= 3 && count <= 4:
		return []string{".few", ".other"}
	case count >= 5 && count <= 19:
		return []string{".many", ".other"}
	default:
		return []string{".other"}
	}
}

// Tf 翻译并格式化（无 Context，使用指定 locale）
func (p *Plugin) Tf(locale, key string, args map[string]any) string {
	text := p.lookup(locale, key)
	if text == "" {
		return key
	}
	return p.render(locale, key, text, args)
}

func (p *Plugin) lookup(locale, key string) string {
	if b, ok := p.bundles.Load(locale); ok {
		if msg, ok := b.(*bundle).msgs[key]; ok {
			return msg
		}
	}
	// 回退到 fallback locale
	if locale != p.cfg.Fallback {
		if b, ok := p.bundles.Load(p.cfg.Fallback); ok {
			if msg, ok := b.(*bundle).msgs[key]; ok {
				return msg
			}
		}
	}
	return ""
}

// render 渲染模板，使用 LRU 缓存预编译结果
func (p *Plugin) render(locale, key, tmplText string, args map[string]any) string {
	cacheKey := locale + "\x00" + key
	var t *template.Template
	if cached, ok := p.tmplCache.Get(cacheKey); ok {
		t = cached
	} else {
		var err error
		t, err = template.New(key).Parse(tmplText)
		if err != nil {
			return tmplText
		}
		p.tmplCache.Add(cacheKey, t)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, args); err != nil {
		return tmplText
	}
	return buf.String()
}
