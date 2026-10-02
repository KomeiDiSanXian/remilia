package songdle

import (
	"errors"
	"testing"
)

func TestTextMark(t *testing.T) {
	cases := []struct {
		target, guess string
		want          Mark
	}{
		{"abc", "abc", Match},
		{"abc", "ab", Close},
		{"ab", "abc", Close},
		{"abc", "xyz", Miss},
		{"", "abc", Miss},
	}
	for _, c := range cases {
		if got := textMark(c.target, c.guess); got != c.want {
			t.Errorf("textMark(%q,%q) = %v, 期望 %v", c.target, c.guess, got, c.want)
		}
	}
}

func TestNumberMark(t *testing.T) {
	if m, d := numberMark(100, 100, 10, 0); m != Match || d != DirNone {
		t.Errorf("相等应为 Match/DirNone，实际 %v/%v", m, d)
	}
	if m, d := numberMark(105, 100, 10, 0); m != Close || d != DirUp {
		t.Errorf("接近且更高应为 Close/DirUp，实际 %v/%v", m, d)
	}
	if m, d := numberMark(90, 100, 10, 0); m != Close || d != DirDown {
		t.Errorf("接近且更低应为 Close/DirDown，实际 %v/%v", m, d)
	}
	if m, d := numberMark(200, 100, 10, 0); m != Miss || d != DirUp {
		t.Errorf("差距过大应为 Miss/DirUp，实际 %v/%v", m, d)
	}
}

func TestVersionMark(t *testing.T) {
	cases := []struct {
		target, guess string
		want          Mark
	}{
		{"maimai", "maimai", Match},
		{"舞萌DX2025", "舞萌DX2024", Close},
		{"maimai DX", "maimai", Close},
		{"舞萌", "音击", Miss},
		{"", "maimai", Miss},
	}
	for _, c := range cases {
		if got := versionMark(c.target, c.guess); got != c.want {
			t.Errorf("versionMark(%q,%q) = %v, 期望 %v", c.target, c.guess, got, c.want)
		}
	}
}

func TestMarkAndDirHelpers(t *testing.T) {
	if Match.Emoji() != "🟩" || Close.Emoji() != "🟨" || Miss.Emoji() != "⬜" {
		t.Error("Mark.Emoji 映射错误")
	}
	if DirUp.Arrow() != "↑" || DirDown.Arrow() != "↓" || DirNone.Arrow() != "" {
		t.Error("Dir.Arrow 映射错误")
	}
}

func TestShareToken(t *testing.T) {
	if !shareToken("kai music", "music team") {
		t.Error("共享词元应判为接近")
	}
	if shareToken("abc", "xyz") {
		t.Error("无关名称不应判为接近")
	}
}

func TestParseAttribute(t *testing.T) {
	cases := map[string]Attribute{
		"曲师":   AttrArtist,
		"bpm":  AttrBPM,
		"BPM":  AttrBPM,
		"定数":   AttrConst,
		"绝赞":   AttrBreak,
		"歌名":   AttrTitle,
		"type": AttrType,
	}
	for in, want := range cases {
		got, ok := ParseAttribute(in)
		if !ok || got != want {
			t.Errorf("ParseAttribute(%q) = %v,%v, 期望 %v", in, got, ok, want)
		}
	}
	if _, ok := ParseAttribute("nope"); ok {
		t.Error("未知属性不应解析成功")
	}
}

func TestAttributeHelpers(t *testing.T) {
	if AttrConst.String() != "const" || AttrConst.LabelKey() != "songdle.attr.const" {
		t.Errorf("AttrConst 标识错误: %q / %q", AttrConst.String(), AttrConst.LabelKey())
	}
	if !AttrBPM.Numeric() || !AttrConst.Numeric() || !AttrBreak.Numeric() {
		t.Error("数值属性应报告 Numeric")
	}
	if AttrArtist.Numeric() || AttrTitle.Numeric() {
		t.Error("非数值属性不应报告 Numeric")
	}
	if len(ClueAttributes) != 8 {
		t.Errorf("可探测属性应为 8 项，实际 %d", len(ClueAttributes))
	}
}

// probeTarget 是 MakeProbe 测试用的固定谜底。
var probeTarget = Track{
	Title: "Future", Artist: "sasakure.UK", Genre: "流行&动漫",
	Type: "SD", Version: "maimai", BPM: 130, MasDS: 10.7, MasBreak: 9,
	Aliases: []string{"未来"},
}

func TestMakeProbeTitle(t *testing.T) {
	pr, err := MakeProbe(probeTarget, AttrTitle, "future")
	if err != nil || pr.Mark != Match || !pr.Solved {
		t.Fatalf("曲名精确匹配应命中: %+v err=%v", pr, err)
	}
	if pr, _ := MakeProbe(probeTarget, AttrTitle, "未来"); pr.Mark != Match {
		t.Errorf("别名应命中，实际 %v", pr.Mark)
	}
	if pr, _ := MakeProbe(probeTarget, AttrTitle, "fut"); pr.Mark != Close || pr.Solved {
		t.Errorf("部分匹配应为 Close 且不算解出，实际 %v solved=%v", pr.Mark, pr.Solved)
	}
	if pr, _ := MakeProbe(probeTarget, AttrTitle, "xyz"); pr.Mark != Miss {
		t.Errorf("无关曲名应为 Miss，实际 %v", pr.Mark)
	}
}

func TestMakeProbeText(t *testing.T) {
	pr, err := MakeProbe(probeTarget, AttrArtist, "sasakure.uk")
	if err != nil || pr.Mark != Match {
		t.Errorf("曲师忽略大小写应完全匹配: %+v err=%v", pr, err)
	}
	if pr, _ := MakeProbe(probeTarget, AttrArtist, "sasakure"); pr.Mark != Close {
		t.Errorf("曲师包含应为 Close，实际 %v", pr.Mark)
	}
	if pr, _ := MakeProbe(probeTarget, AttrGenre, "流行"); pr.Mark != Close {
		t.Errorf("流派包含应为 Close，实际 %v", pr.Mark)
	}
	if pr, _ := MakeProbe(probeTarget, AttrType, "sd"); pr.Mark != Match || pr.Value != "SD" {
		t.Errorf("类型应归一化为大写并匹配: %+v", pr)
	}
	if pr, _ := MakeProbe(probeTarget, AttrType, "DX"); pr.Mark != Miss {
		t.Errorf("类型不同应为 Miss，实际 %v", pr.Mark)
	}
	if pr, _ := MakeProbe(probeTarget, AttrVersion, "maimai"); pr.Mark != Match {
		t.Errorf("版本相同应为 Match，实际 %v", pr.Mark)
	}
}

func TestMakeProbeNumeric(t *testing.T) {
	pr, err := MakeProbe(probeTarget, AttrBPM, "130")
	if err != nil || pr.Mark != Match || pr.Dir != DirNone {
		t.Fatalf("BPM 相等应为 Match: %+v err=%v", pr, err)
	}
	if pr, _ := MakeProbe(probeTarget, AttrBPM, "135"); pr.Mark != Close || pr.Dir != DirDown {
		t.Errorf("BPM 135 应 Close/↓，实际 %v/%v", pr.Mark, pr.Dir)
	}
	if pr, _ := MakeProbe(probeTarget, AttrBPM, "100"); pr.Mark != Miss || pr.Dir != DirUp {
		t.Errorf("BPM 100 应 Miss/↑，实际 %v/%v", pr.Mark, pr.Dir)
	}

	if pr, _ := MakeProbe(probeTarget, AttrConst, "10.5"); pr.Mark != Close || pr.Dir != DirUp {
		t.Errorf("定数 10.5 应 Close/↑，实际 %v/%v", pr.Mark, pr.Dir)
	}
	if pr, _ := MakeProbe(probeTarget, AttrConst, "10.7"); pr.Mark != Match {
		t.Errorf("定数相等应 Match，实际 %v", pr.Mark)
	}

	if pr, _ := MakeProbe(probeTarget, AttrBreak, "7"); pr.Mark != Close || pr.Dir != DirUp {
		t.Errorf("绝赞 7 应 Close/↑，实际 %v/%v", pr.Mark, pr.Dir)
	}
	if pr, _ := MakeProbe(probeTarget, AttrBreak, "4"); pr.Mark != Miss || pr.Dir != DirUp {
		t.Errorf("绝赞 4 应 Miss/↑，实际 %v/%v", pr.Mark, pr.Dir)
	}
}

func TestMakeProbeBadValue(t *testing.T) {
	cases := []struct {
		attr Attribute
		raw  string
	}{
		{AttrTitle, ""},
		{AttrBPM, "abc"},
		{AttrBPM, "0"},
		{AttrConst, "abc"},
		{AttrConst, "-1"},
		{AttrBreak, "-1"},
		{AttrType, "XX"},
	}
	for _, c := range cases {
		if _, err := MakeProbe(probeTarget, c.attr, c.raw); !errors.Is(err, ErrBadValue) {
			t.Errorf("MakeProbe(%v,%q) 应返回 ErrBadValue，实际 %v", c.attr, c.raw, err)
		}
	}
}

func TestProbeNumber(t *testing.T) {
	if n, ok := ProbeNumber(AttrBPM, "150"); !ok || n != 150 {
		t.Errorf("ProbeNumber(bpm,150) = %v,%v", n, ok)
	}
	if n, ok := ProbeNumber(AttrConst, "13.5"); !ok || n != 13.5 {
		t.Errorf("ProbeNumber(const,13.5) = %v,%v", n, ok)
	}
	if _, ok := ProbeNumber(AttrArtist, "x"); ok {
		t.Error("非数值属性不应返回数值")
	}
}

func TestFormatDS(t *testing.T) {
	if got := formatDS(12.4); got != "12.4" {
		t.Errorf("formatDS(12.4) = %q", got)
	}
	if got := formatDS(0); got != "—" {
		t.Errorf("formatDS(0) = %q, 期望占位符", got)
	}
}

// compareTarget 是 Compare / TrackGuess 测试用的固定谜底。
var compareTarget = Track{
	ID: "3", Title: "Future", Artist: "sasakure.UK", Genre: "流行&动漫",
	Type: "SD", Version: "舞萌DX2024", BPM: 130, MasCharter: "Q", MasDS: 10.7, MasBreak: 9,
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		target, guess string
		wantMark      Mark
		wantDir       Dir
	}{
		{"舞萌DX2024", "舞萌DX2024", Match, DirNone},
		{"舞萌DX2024", "舞萌DX2023", Close, DirUp},   // 相邻且谜底更新
		{"舞萌DX2024", "舞萌DX2025", Close, DirDown}, // 相邻且谜底更旧
		{"舞萌DX2024", "舞萌DX2022", Miss, DirUp},    // 相隔过远
		{"舞萌DX2024", "maimai", Miss, DirUp},      // 跨大版本
		{"maimai DX", "maimai", Close, DirNone},  // 未知版本回退包含匹配
		{"", "舞萌DX2024", Miss, DirNone},          // 空版本不比较
	}
	for _, c := range cases {
		mark, dir := versionCompare(c.target, c.guess)
		if mark != c.wantMark || dir != c.wantDir {
			t.Errorf("versionCompare(%q,%q) = %v/%v, 期望 %v/%v",
				c.target, c.guess, mark, dir, c.wantMark, c.wantDir)
		}
	}
}

func TestCompareColumns(t *testing.T) {
	cells := Compare(compareTarget, compareTarget)
	if len(cells) != len(GuessColumns) {
		t.Fatalf("列数 = %d, 期望 %d", len(cells), len(GuessColumns))
	}
	for i, c := range cells {
		if c.Attr != GuessColumns[i] {
			t.Errorf("第 %d 列属性 = %v, 期望 %v", i, c.Attr, GuessColumns[i])
		}
		if c.Mark != Match {
			t.Errorf("猜中自身时 %v 列应为 Match，实际 %v", c.Attr, c.Mark)
		}
	}

	tg := TrackGuess{Track: compareTarget, Cells: cells}
	if !tg.Solved() {
		t.Error("猜中自身应判为已解出")
	}
	if c, ok := tg.Cell(AttrTitle); !ok || c.Value != "Future" {
		t.Errorf("曲名列应为 Future，实际 %+v ok=%v", c, ok)
	}
	if _, ok := tg.Cell(Attribute(200)); ok {
		t.Error("不存在的列应返回 ok=false")
	}
}

func TestCompareMarksAndDirections(t *testing.T) {
	target := compareTarget
	guess := Track{
		ID: "9", Title: "Other", Artist: "sasakure", Genre: "舞萌",
		Type: "dx", Version: "舞萌DX2025", BPM: 100, MasCharter: "Q", MasDS: 10.2, MasBreak: 4,
	}
	cells := Compare(target, guess)
	byAttr := func(a Attribute) Cell {
		t.Helper()
		for _, c := range cells {
			if c.Attr == a {
				return c
			}
		}
		t.Fatalf("缺少 %v 列", a)
		return Cell{}
	}

	if c := byAttr(AttrTitle); c.Mark != Miss || c.Value != "Other" {
		t.Errorf("不同曲名应为 Miss 且展示猜测值，实际 %+v", c)
	}
	if c := byAttr(AttrType); c.Mark != Miss || c.Value != "DX" {
		t.Errorf("类型应归一化为大写，实际 %+v", c)
	}
	if c := byAttr(AttrArtist); c.Mark != Close {
		t.Errorf("曲师包含应为 Close，实际 %+v", c)
	}
	if c := byAttr(AttrGenre); c.Mark != Miss {
		t.Errorf("无关流派应为 Miss，实际 %+v", c)
	}
	if c := byAttr(AttrVersion); c.Mark != Close || c.Dir != DirDown {
		t.Errorf("相邻版本且所猜更新应为 Close/↓，实际 %+v", c)
	}
	if c := byAttr(AttrBPM); c.Mark != Miss || c.Dir != DirUp || c.Value != "100" {
		t.Errorf("BPM 100 应 Miss/↑，实际 %+v", c)
	}
	if c := byAttr(AttrConst); c.Mark != Close || c.Dir != DirUp || c.Value != "10.2" {
		t.Errorf("定数 10.2 应 Close/↑，实际 %+v", c)
	}
	if c := byAttr(AttrBreak); c.Mark != Miss || c.Dir != DirUp {
		t.Errorf("绝赞 4 应 Miss/↑，实际 %+v", c)
	}
}

func TestTrackGuessSolvedWithoutTitle(t *testing.T) {
	if (TrackGuess{}).Solved() {
		t.Error("空猜测不应判为已解出")
	}
	tg := TrackGuess{Cells: []Cell{{Attr: AttrArtist, Mark: Match}}}
	if tg.Solved() {
		t.Error("缺少曲名列不应判为已解出")
	}
}
