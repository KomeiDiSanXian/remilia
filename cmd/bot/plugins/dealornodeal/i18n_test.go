package dealornodeal

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
)

func TestRegisterLocales(t *testing.T) {
	svc := i18n.NewPlugin(i18n.Config{DefaultLocale: localeZH, Fallback: localeZH})
	if err := registerLocales(svc); err != nil {
		t.Fatalf("registerLocales: %v", err)
	}
	// 代码中实际引用的全部 key，两个语言包都必须提供。
	keys := []string{
		"dond.help", "dond.rules",
		"dond.start.running", "dond.start.bad_scope", "dond.start.bad_cases",
		"dond.start.created_single", "dond.start.created_group",
		"dond.pick.prompt", "dond.pick.done", "dond.pick.random_done",
		"dond.open.prompt", "dond.open.phase", "dond.open.bad",
		"dond.open.opened", "dond.open.own", "dond.open.done",
		"dond.open.batch", "dond.open.batch_filled", "dond.open.batch_skipped",
		"dond.open.skip.opened", "dond.open.skip.own", "dond.open.skip.bad",
		"dond.open.skip.notnum", "dond.open.skip.round_full",
		"dond.offer.header", "dond.offer.hint_single", "dond.offer.hint_group",
		"dond.offer.vote_cast", "dond.offer.resolved_deal", "dond.offer.resolved_nodeal",
		"dond.offer.final",
		"dond.deal.done", "dond.nodeal.next", "dond.nodeal.final",
		"dond.final.prompt", "dond.final.swap", "dond.final.keep",
		"dond.result.dealt", "dond.result.reveal",
		"dond.board.none", "dond.quit.done", "dond.quit.denied", "dond.resolve.denied", "dond.not_owner",
		"dond.lang.done", "dond.lang.usage", "dond.unknown_sub", "dond.error.generic",
		"dond.button.pick", "dond.button.pick_random", "dond.button.open",
		"dond.button.board", "dond.button.rules", "dond.button.deal",
		"dond.button.nodeal", "dond.button.swap", "dond.button.keep",
		"dond.button.new", "dond.button.resolve", "dond.button.open_tip",
	}
	for _, loc := range SupportedLocales {
		for _, key := range keys {
			if got := svc.Tf(loc, key, nil); got == "" || got == key {
				t.Errorf("locale %s 缺少 %s", loc, key)
			}
		}
	}
	// 重复合并应幂等。
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
	if localeList() == "" {
		t.Error("localeList 不应为空")
	}
}
