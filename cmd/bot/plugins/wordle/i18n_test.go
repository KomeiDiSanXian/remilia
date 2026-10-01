package wordle

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
)

func TestRegisterLocales(t *testing.T) {
	svc := i18n.NewPlugin(i18n.Config{DefaultLocale: localeZH, Fallback: localeZH})
	if err := registerLocales(svc); err != nil {
		t.Fatalf("registerLocales: %v", err)
	}
	for _, loc := range SupportedLocales {
		got := svc.Tf(loc, "wordle.button.guess", nil)
		if got == "" || got == "wordle.button.guess" {
			t.Errorf("locale %s 缺少 wordle.button.guess", loc)
		}
	}
	// 新增玩法相关文案在两个语言包中都必须存在。
	keys := []string{
		"wordle.rule.hard", "wordle.rule.blitz", "wordle.rule.chain",
		"wordle.rule.hint-cost", "wordle.rule.chaos", "wordle.rule.race",
		"wordle.rule.blind", "wordle.rule.fog", "wordle.rule.obscure", "wordle.rule.gauntlet",
		"wordle.rule.invert", "wordle.rule.hit-only", "wordle.rule.near", "wordle.rule.repeat",
		"wordle.rule.swap-meaning", "wordle.rule.colorfog", "wordle.rule.decay",
		"wordle.rule.unknown", "wordle.rule.delayed", "wordle.rule.decoy", "wordle.rule.glitch",
		"wordle.rule.mole", "wordle.rule.hidden-key", "wordle.rule.score-color",
		"wordle.mod.blind", "wordle.mod.norepeat", "wordle.mod.double", "wordle.mod.nohint",
		"wordle.result.timeout", "wordle.chain.advanced", "wordle.error.hard_position",
		"wordle.error.chain_daily", "wordle.error.race_needs_group", "wordle.start.bad_blitz",
		"wordle.start.bad_fog", "wordle.start.bad_boards", "wordle.result.win_multi",
		"wordle.start.bad_range",
		"wordle.hint.exclude", "wordle.hint.vowels", "wordle.hint.repeat.some",
		"wordle.race.line", "wordle.rules.title", "wordle.button.rules",
	}
	for _, loc := range SupportedLocales {
		for _, key := range keys {
			if got := svc.Tf(loc, key, nil); got == "" || got == key {
				t.Errorf("locale %s 缺少 %s", loc, key)
			}
		}
	}
	// 重复合并应幂等（MergeBytes 覆盖同名键）。
	if err := registerLocales(svc); err != nil {
		t.Fatalf("重复 registerLocales 失败: %v", err)
	}
}

func TestNormalizeLocale(t *testing.T) {
	cases := map[string]string{
		"zh":    localeZH,
		"cn":    localeZH,
		"zh-CN": localeZH,
		"en":    localeEN,
		"EN-us": localeEN,
		"fr":    "fr",
	}
	for in, want := range cases {
		if got := normalizeLocale(in); got != want {
			t.Errorf("normalizeLocale(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestIsSupportedLocale(t *testing.T) {
	if !isSupportedLocale(localeZH) || !isSupportedLocale(localeEN) {
		t.Error("内置语言应被支持")
	}
	if isSupportedLocale("fr") {
		t.Error("fr 未内置，不应被支持")
	}
}
