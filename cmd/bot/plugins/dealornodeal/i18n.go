package dealornodeal

import (
	"embed"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

//go:embed locales/*.yaml
var localesFS embed.FS

const (
	localeZH = "zh-CN"
	localeEN = "en-US"
)

// SupportedLocales 插件内置的语言包列表。
var SupportedLocales = []string{localeZH, localeEN}

// registerLocales 把插件自带语言包增量合并进 i18n 插件。
//
// 使用 MergeBytes 而非 LoadBytes：多个插件可各自向同一 locale 贡献 key，
// 不会相互覆盖；本插件所有 key 以 "dond." 为前缀避免冲突。
func registerLocales(svc *i18n.Plugin) error {
	for _, loc := range SupportedLocales {
		data, err := localesFS.ReadFile("locales/" + loc + ".yaml")
		if err != nil {
			return fmt.Errorf("dealornodeal: 读取语言包 %s: %w", loc, err)
		}
		if err := svc.MergeBytes(loc, data); err != nil {
			return err
		}
	}
	return nil
}

// t 翻译 key；未接入 i18n 时回退为 key 本身，保证命令不因缺包而失败。
func (p *Plugin) t(ctx *eventctx.Context, key string, args ...map[string]any) string {
	if p.i18n == nil {
		return key
	}
	return p.i18n.T(ctx, key, args...)
}

// localeList 返回以 "/" 连接的语言包列表，用于提示文案。
func localeList() string {
	return strings.Join(SupportedLocales, " / ")
}
