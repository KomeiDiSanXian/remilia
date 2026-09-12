package fortune

import (
	"strings"
	"testing"
)

// TestOmikujiSlipsComplete 确认 100 番签文数据完整：番号连续、吉凶合法、
// 漢詩四句、译意与分类运势均非空。
func TestOmikujiSlipsComplete(t *testing.T) {
	if len(omikujiSlips) != 100 {
		t.Fatalf("签文应有 100 番，实际 %d", len(omikujiSlips))
	}

	valid := map[Level]bool{
		Daikichi: true, Sueshokichi: true, Shokichi: true,
		Hankichi: true, Suekichi: true, Kichi: true, Kyo: true,
	}

	for i, s := range omikujiSlips {
		number := i + 1
		if s.Number != number {
			t.Errorf("第 %d 项签号应为 %d，实际 %d", number, number, s.Number)
		}
		if !valid[s.Level] {
			t.Errorf("第 %d 番吉凶非法: %v", number, s.Level)
		}
		for j, line := range s.Poem {
			if strings.TrimSpace(line) == "" {
				t.Errorf("第 %d 番漢詩第 %d 句为空", number, j+1)
			}
		}
		if strings.TrimSpace(s.PoemZH) == "" {
			t.Errorf("第 %d 番缺少译意", number)
		}
		if len(s.Items) == 0 {
			t.Errorf("第 %d 番缺少分类运势", number)
		}
		for _, it := range s.Items {
			if strings.TrimSpace(it.Name) == "" || strings.TrimSpace(it.Value) == "" {
				t.Errorf("第 %d 番存在空的运势项: %+v", number, it)
			}
		}
	}
}

// TestOmikujiLevelDistribution 锁定吉凶档位的分布，作为数据集指纹。
//
// 真实签面只有七档，且凶签约占三成 —— 这是浅草寺观音签的特征。
// 若此处失败，说明签文数据被替换或损坏，需重新核对来源。
func TestOmikujiLevelDistribution(t *testing.T) {
	want := map[Level]int{
		Daikichi: 17, Kichi: 35, Suekichi: 6, Hankichi: 5,
		Shokichi: 5, Sueshokichi: 2, Kyo: 30,
	}

	got := map[Level]int{}
	for _, s := range omikujiSlips {
		got[s.Level]++
	}

	for level, n := range want {
		if got[level] != n {
			t.Errorf("%s 应有 %d 番，实际 %d", level, n, got[level])
		}
	}
	for level, n := range got {
		if want[level] == 0 {
			t.Errorf("出现预期外的吉凶 %s（%d 番）", level, n)
		}
	}
}

// TestOmikujiSlipLookup 确认签号查表与越界处理。
func TestOmikujiSlipLookup(t *testing.T) {
	for _, n := range []int{0, -1, 101, 1000} {
		if slip := omikujiSlip(n); slip != nil {
			t.Errorf("签号 %d 越界应返回 nil，实际 %+v", n, slip)
		}
	}
	for _, n := range []int{1, 42, 100} {
		slip := omikujiSlip(n)
		if slip == nil {
			t.Fatalf("签号 %d 应有数据", n)
		}
		if slip.Number != n {
			t.Errorf("签号 %d 取到第 %d 番", n, slip.Number)
		}
	}
}

// TestOmikujiKnownSlips 抽查番号，与签纸扫描件逐条核对过的结果比对。
func TestOmikujiKnownSlips(t *testing.T) {
	cases := []struct {
		number int
		level  Level
		first  string
	}{
		{1, Daikichi, "七宝浮图塔"},
		{2, Shokichi, "月被浮云翳"},
		{42, Kichi, "桂华春将到"},
		{70, Kyo, "雷发庭前草"},
	}

	for _, c := range cases {
		slip := omikujiSlip(c.number)
		if slip == nil {
			t.Fatalf("签号 %d 应有数据", c.number)
		}
		if slip.Level != c.level {
			t.Errorf("第 %d 番吉凶应为 %s，实际 %s", c.number, c.level, slip.Level)
		}
		if slip.Poem[0] != c.first {
			t.Errorf("第 %d 番首句应为 %q，实际 %q", c.number, c.first, slip.Poem[0])
		}
	}
}

// TestFormatOmikujiSections 确认两种排版都包含各分区与末尾的固定说明，
// 且凶签提示只出现在凶签上。
func TestFormatOmikujiSections(t *testing.T) {
	daikichi := omikujiSlip(1)
	kyo := omikujiSlip(70)

	md := formatOmikujiMD(daikichi)
	for _, want := range []string{
		"第 1 番", "大吉", "汉诗", "签意", "各项运势",
		omikujiSource, omikujiDisclaimer,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown 解签缺少 %q", want)
		}
	}
	if strings.Contains(md, omikujiKyoHint) {
		t.Error("大吉不应出现凶签提示")
	}
	// 分类运势以表格呈现，至少有表头分隔行。
	if !strings.Contains(md, ":---:") {
		t.Error("Markdown 解签应包含运势表格")
	}

	if mdKyo := formatOmikujiMD(kyo); !strings.Contains(mdKyo, omikujiKyoHint) {
		t.Error("凶签的 Markdown 解签应包含凶签提示")
	}

	text := formatOmikujiText(daikichi)
	for _, want := range []string{
		"第 1 番", "汉诗", "签意", "愿望",
		omikujiSource, omikujiDisclaimer,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("纯文本解签缺少 %q", want)
		}
	}
	if strings.Contains(text, "|") {
		t.Error("纯文本解签不应包含表格分隔符")
	}
	if txtKyo := formatOmikujiText(kyo); !strings.Contains(txtKyo, omikujiKyoHint) {
		t.Error("凶签的纯文本解签应包含凶签提示")
	}
}

// TestMdCellEscapes 确认会破坏表格排版的字符被转义。
func TestMdCellEscapes(t *testing.T) {
	if got := mdCell("a|b"); got != `a\|b` {
		t.Errorf("竖线应被转义，实际 %q", got)
	}
	if got := mdCell("a\nb"); got != "a b" {
		t.Errorf("换行应被替换为空格，实际 %q", got)
	}
}
