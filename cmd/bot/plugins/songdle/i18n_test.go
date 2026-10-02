package songdle

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
)

// localeKeys 是插件代码中引用到的全部 i18n key，用于校验语言包完整性。
var localeKeys = []string{
	"songdle.help",
	"songdle.mode.daily",
	"songdle.mode.random",
	"songdle.start.created",
	"songdle.start.already_running",
	"songdle.start.bad_tries",
	"songdle.start.bad_scope",
	"songdle.start.bad_type",
	"songdle.start.empty_pool",
	"songdle.start.daily_done",
	"songdle.board.header",
	"songdle.board.masked",
	"songdle.board.clues_title",
	"songdle.board.history_title",
	"songdle.board.no_probes",
	"songdle.board.legend_img",
	"songdle.clue.known",
	"songdle.clue.range",
	"songdle.clue.range_both",
	"songdle.clue.range_lo",
	"songdle.clue.range_hi",
	"songdle.clue.excluded",
	"songdle.clue.unknown",
	"songdle.probe.line",
	"songdle.answer",
	"songdle.attr.title",
	"songdle.attr.artist",
	"songdle.attr.genre",
	"songdle.attr.type",
	"songdle.attr.version",
	"songdle.attr.bpm",
	"songdle.attr.const",
	"songdle.attr.break",
	"songdle.mark.match",
	"songdle.mark.close",
	"songdle.mark.miss",
	"songdle.mark.unknown",
	"songdle.table.attribute",
	"songdle.table.value",
	"songdle.table.verdict",
	"songdle.table.index",
	"songdle.probe.win",
	"songdle.probe.lose",
	"songdle.probe.title_close",
	"songdle.probe.title_miss",
	"songdle.probe.recorded",
	"songdle.probe.repeat",
	"songdle.probe.bad_value",
	"songdle.probe.usage",
	"songdle.probe.bad_attr",
	"songdle.error.not_started",
	"songdle.error.already_finished",
	"songdle.error.usage_guess",
	"songdle.giveup.denied",
	"songdle.giveup.done",
	"songdle.pool.total",
	"songdle.pool.row",
	"songdle.pool.empty",
	"songdle.stats.title",
	"songdle.stats.body",
	"songdle.stats.dist.title",
	"songdle.stats.dist.fail",
	"songdle.stats.dist.row",
	"songdle.stats.unavailable",
	"songdle.stats.error",
	"songdle.top.title",
	"songdle.top.row",
	"songdle.top.empty",
	"songdle.top.unavailable",
	"songdle.top.error",
	"songdle.lang.usage",
	"songdle.lang.unknown",
	"songdle.lang.set",
	"songdle.button.title",
	"songdle.button.title_tip",
	"songdle.button.guess_id",
	"songdle.button.guess_id_tip",
	"songdle.button.bpm",
	"songdle.button.bpm_tip",
	"songdle.button.artist",
	"songdle.button.artist_tip",
	"songdle.button.giveup",
	"songdle.button.board",
	"songdle.button.new",
}

func newI18nPlugin(t *testing.T) *i18n.Plugin {
	t.Helper()
	svc := i18n.NewPlugin(i18n.Config{DefaultLocale: localeZH, Fallback: localeZH})
	if err := registerLocales(svc); err != nil {
		t.Fatalf("registerLocales: %v", err)
	}
	return svc
}

func TestLocalesCoverAllKeys(t *testing.T) {
	svc := newI18nPlugin(t)
	for _, loc := range SupportedLocales {
		for _, key := range localeKeys {
			if got := svc.Tf(loc, key, nil); got == "" || got == key {
				t.Errorf("[%s] 缺少 key %q", loc, key)
			}
		}
	}
}

func TestLocaleTemplatesRender(t *testing.T) {
	svc := newI18nPlugin(t)
	cases := []struct {
		key  string
		args map[string]any
	}{
		{"songdle.start.created", map[string]any{"Mode": "随机题", "Tries": 12, "Type": "DX", "Genre": "东方"}},
		{"songdle.start.created", map[string]any{"Mode": "随机题", "Tries": 12, "Type": "", "Genre": ""}},
		{"songdle.start.empty_pool", map[string]any{"Type": "", "Genre": ""}},
		{"songdle.start.empty_pool", map[string]any{"Type": "DX", "Genre": "东方"}},
		{"songdle.board.header", map[string]any{"Mode": "随机题", "Attempts": 3, "Max": 12}},
		{"songdle.board.masked", map[string]any{"Mask": "⬛⬛⬛", "Len": 3}},
		{"songdle.clue.known", map[string]any{"Mark": "🟩", "Label": "曲师", "Value": "Kai"}},
		{"songdle.clue.range", map[string]any{"Mark": "🟨", "Label": "BPM", "Range": "大于 150"}},
		{"songdle.clue.range_both", map[string]any{"Lo": 150, "Hi": 200}},
		{"songdle.clue.range_lo", map[string]any{"Lo": 150}},
		{"songdle.clue.range_hi", map[string]any{"Hi": 200}},
		{"songdle.clue.excluded", map[string]any{"Mark": "⬜", "Label": "流派", "Values": "舞萌 / 东方"}},
		{"songdle.clue.unknown", map[string]any{"Label": "定数"}},
		{"songdle.probe.line", map[string]any{"Mark": "🟨", "Label": "BPM", "Value": "150", "Arrow": "↓"}},
		{"songdle.probe.win", map[string]any{"Attempts": 5}},
		{"songdle.probe.title_close", map[string]any{"Remaining": 4}},
		{"songdle.probe.title_miss", map[string]any{"Remaining": 4}},
		{"songdle.probe.recorded", map[string]any{"Remaining": 4}},
		{"songdle.probe.repeat", map[string]any{"Attr": "BPM", "Value": "150"}},
		{"songdle.probe.bad_value", map[string]any{"Attr": "BPM", "Value": "abc"}},
		{"songdle.probe.usage", map[string]any{"Attrs": "曲师 / 流派"}},
		{"songdle.probe.bad_attr", map[string]any{"Value": "xx", "Attrs": "曲师 / 流派"}},
		{"songdle.pool.total", map[string]any{"Total": 1175, "Attrs": "曲师 / 流派"}},
		{"songdle.stats.body", map[string]any{"Played": 3, "Won": 2, "Rate": "66.7", "Streak": 1, "Best": 2, "Fewest": 3}},
		{"songdle.top.row", map[string]any{"Rank": 1, "User": "甲", "Won": 5, "Played": 6, "Rate": "83", "Best": 2}},
		{"songdle.answer", map[string]any{"Answer": "Future — X"}},
	}
	for _, c := range cases {
		for _, loc := range SupportedLocales {
			got := svc.Tf(loc, c.key, c.args)
			if strings.Contains(got, "{{") || strings.Contains(got, "<no value>") {
				t.Errorf("[%s] %s 模板渲染异常: %q", loc, c.key, got)
			}
		}
	}
}

// TestLocaleConditionalTemplates 验证 {{if}} 分支在有无取值时都能正确渲染。
func TestLocaleConditionalTemplates(t *testing.T) {
	svc := newI18nPlugin(t)
	full := svc.Tf(localeZH, "songdle.start.created",
		map[string]any{"Mode": "随机题", "Tries": 12, "Type": "DX", "Genre": "东方"})
	if !strings.Contains(full, "DX") || !strings.Contains(full, "东方") {
		t.Errorf("带筛选条件的开局文案应包含类型与流派: %q", full)
	}
	plain := svc.Tf(localeZH, "songdle.start.created",
		map[string]any{"Mode": "随机题", "Tries": 12, "Type": "", "Genre": ""})
	if strings.Contains(plain, "类型") || strings.Contains(plain, "流派") {
		t.Errorf("无筛选条件时不应展示类型 / 流派: %q", plain)
	}
	emptyPool := svc.Tf(localeZH, "songdle.start.empty_pool", map[string]any{"Type": "", "Genre": ""})
	if strings.Contains(emptyPool, "类型") {
		t.Errorf("无筛选条件时不应展示类型: %q", emptyPool)
	}
}

func TestNormalizeLocale(t *testing.T) {
	cases := map[string]string{
		"zh":    localeZH,
		"ZH-CN": localeZH,
		"cn":    localeZH,
		"en":    localeEN,
		"en-gb": localeEN,
		"fr":    "fr",
	}
	for in, want := range cases {
		if got := normalizeLocale(in); got != want {
			t.Errorf("normalizeLocale(%q) = %q, 期望 %q", in, got, want)
		}
	}
	if !isSupportedLocale(localeZH) || !isSupportedLocale(localeEN) {
		t.Error("内置语言应被识别")
	}
	if isSupportedLocale("fr") {
		t.Error("未内置的语言不应被识别")
	}
}
