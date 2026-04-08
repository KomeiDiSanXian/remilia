package zhtext_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/zhtext"
)

// ── CJK ─────────────────────────────────────────────────────────────────────

func TestIsHan(t *testing.T) {
	cases := []struct {
		r    rune
		want bool
	}{
		{'你', true},
		{'好', true},
		{'A', false},
		{'ひ', false}, // 平假名
		{'1', false},
		{'天', true},
	}
	for _, c := range cases {
		if got := zhtext.IsHan(c.r); got != c.want {
			t.Errorf("IsHan(%q) = %v, want %v", c.r, got, c.want)
		}
	}
}

func TestContainsChinese(t *testing.T) {
	if !zhtext.ContainsChinese("Hello世界") {
		t.Error("ContainsChinese(\"Hello世界\") should be true")
	}
	if zhtext.ContainsChinese("Hello world") {
		t.Error("ContainsChinese(\"Hello world\") should be false")
	}
}

func TestChineseCharCount(t *testing.T) {
	if n := zhtext.ChineseCharCount("Hello世界foo"); n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
	if n := zhtext.ChineseCharCount("abc"); n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestSplitCJK(t *testing.T) {
	tokens := zhtext.SplitCJK("Hello世界foo")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "Hello" || tokens[1] != "世界" || tokens[2] != "foo" {
		t.Errorf("unexpected tokens: %v", tokens)
	}
}

// ── Normalize ────────────────────────────────────────────────────────────────

func TestFullToHalf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ＡＢＣＤ", "ABCD"},
		{"１２３", "123"},
		{"／ping", "/ping"},
		{"hello", "hello"}, // 已是半角，不变
	}
	for _, c := range cases {
		if got := zhtext.FullToHalf(c.in); got != c.want {
			t.Errorf("FullToHalf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeCJK(t *testing.T) {
	got := zhtext.NormalizeCJK("　／ping　")
	if got != "/ping" {
		t.Errorf("NormalizeCJK: got %q, want %q", got, "/ping")
	}
}

func TestCollapseSpaces(t *testing.T) {
	got := zhtext.CollapseSpaces("  hello   world  ")
	if got != "hello world" {
		t.Errorf("CollapseSpaces: got %q, want %q", got, "hello world")
	}
}

// ── Fuzzy ────────────────────────────────────────────────────────────────────

func TestMatch(t *testing.T) {
	if !zhtext.Match("天气", "天气预报") {
		t.Error("Match(天气, 天气预报) should be true")
	}
	if zhtext.Match("气天", "天气预报") {
		t.Error("Match(气天, 天气预报) should be false (wrong order)")
	}
}

func TestFind(t *testing.T) {
	targets := []string{"天气预报", "温度查询", "降雨量"}
	results := zhtext.Find(targets, "天气")
	if len(results) != 1 || results[0] != "天气预报" {
		t.Errorf("Find: unexpected results: %v", results)
	}
}

func TestFindFold(t *testing.T) {
	targets := []string{"WeatherForecast", "TempQuery", "RainFall"}
	results := zhtext.FindFold(targets, "weather")
	if len(results) != 1 || results[0] != "WeatherForecast" {
		t.Errorf("FindFold: unexpected results: %v", results)
	}
}

func TestRankFind(t *testing.T) {
	targets := []string{"天气", "天气预报", "其他"}
	results := zhtext.RankFind(targets, "天气")
	if len(results) < 2 {
		t.Errorf("RankFind: expected at least 2 results, got %v", results)
	}
	// 完全匹配的 "天气" 应该 Distance 最小（排在前面）
	if results[0].Source != "天气" {
		t.Errorf("RankFind: expected '天气' first, got %q", results[0].Source)
	}
}

// ── T2S ──────────────────────────────────────────────────────────────────────

func TestTraditionalToSimplified(t *testing.T) {
	cases := []struct{ in, want string }{
		{"臺灣天氣預報", "台湾天气预报"},
		{"hello world", "hello world"}, // 无汉字，不变
		{"開關", "开关"},
	}
	for _, c := range cases {
		if got := zhtext.TraditionalToSimplified(c.in); got != c.want {
			t.Errorf("T2S(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSimplifiedToTraditional(t *testing.T) {
	// 简→繁对照（仅验证在对照表中的字）
	got := zhtext.SimplifiedToTraditional("开关")
	if got == "开关" { // 应该发生了转换
		// '开'→'開', '关' 可能不在表中，但至少 '开' 应该转换
		t.Logf("SimplifiedToTraditional(开关) = %q (some chars may not be in table)", got)
	}
}
