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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"gopkg.in/yaml.v3"
)

// localeKey 存储用户语言偏好的 Context key
const localeKey = "_i18n_locale"

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
}

// New 创建 i18n 插件描述符
// New creates the i18n plugin descriptor.
// Use NewPlugin() to also get a direct reference to the Plugin API.
func New(cfg Config) *plugin.PluginDescriptor {
	_, desc := NewPlugin(cfg)
	return desc
}

// NewPlugin creates the i18n plugin and returns both the Plugin API and its descriptor.
func NewPlugin(cfg Config) (*Plugin, *plugin.PluginDescriptor) {
	if cfg.DefaultLocale == "" {
		cfg.DefaultLocale = "zh-CN"
	}
	if cfg.Fallback == "" {
		cfg.Fallback = cfg.DefaultLocale
	}

	p := &Plugin{cfg: cfg}

	desc := &plugin.PluginDescriptor{
		Name:        "i18n",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "国际化/本地化插件，支持多语言文本和热更新",
		Category:    "核心",
		Tags:        []string{"i18n", "国际化", "多语言"},
		Deps:        []string{},
		HelpText: `i18n 插件使用说明：
  t := ctx.MustGet("i18n").(*i18n.Plugin)
  msg := t.T(ctx, "key")
  msg := t.T(ctx, "key", map[string]any{"k":"v"})
  t.SetLocale(ctx, "en-US")`,

		Setup: func(setupCtx *plugin.SetupContext) error {
			logger.Infof("[i18n] Loading locales from '%s', default=%s", cfg.LocaleDir, cfg.DefaultLocale)
			if cfg.LocaleDir != "" {
				if err := p.loadDir(cfg.LocaleDir); err != nil {
					logger.WithError(err).Warn("[i18n] Failed to load locale dir, continuing with empty bundles")
				}
			}
			setupCtx.Manager.GetContainer().Register("i18n", p)
			logger.Info("[i18n] Plugin loaded")
			return nil
		},

		Reload: func(setupCtx *plugin.SetupContext) error {
			if cfg.LocaleDir != "" {
				return p.loadDir(cfg.LocaleDir)
			}
			return nil
		},
	}
	return p, desc
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
func (p *Plugin) LoadBytes(locale string, data []byte) error {
	var msgs map[string]string
	if err := yaml.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("i18n: parse yaml for %s: %w", locale, err)
	}
	p.bundles.Store(locale, &bundle{locale: locale, msgs: msgs})
	logger.Debugf("[i18n] Loaded locale %s (%d messages)", locale, len(msgs))
	return nil
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
		return key // 返回 key 本身作为 fallback
	}
	if len(args) == 0 {
		return text
	}
	return p.render(text, args[0])
}

// Tf 翻译并格式化（无 Context，使用默认 locale）
func (p *Plugin) Tf(locale, key string, args map[string]any) string {
	text := p.lookup(locale, key)
	if text == "" {
		return key
	}
	return p.render(text, args)
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

func (p *Plugin) render(tmpl string, args map[string]any) string {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return tmpl
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, args); err != nil {
		return tmpl
	}
	return buf.String()
}
