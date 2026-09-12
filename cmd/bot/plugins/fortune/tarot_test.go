package fortune

import (
	"bytes"
	"image"
	"strings"
	"testing"
	"unicode"
)

// TestTarotDeckComplete 确认 78 张牌的牌位、关键词与解读都不缺。
func TestTarotDeckComplete(t *testing.T) {
	if len(tarotDeck) != 78 {
		t.Fatalf("牌库应有 78 张牌，实际 %d", len(tarotDeck))
	}

	majors := 0
	ranks := map[int]int{}
	for short, card := range tarotDeck {
		if card.MeaningUp == "" || card.MeaningRev == "" {
			t.Errorf("%s 缺少正位或逆位关键词", short)
		}
		if card.Reading(false) == "" || card.Reading(true) == "" {
			t.Errorf("%s 缺少正位或逆位解读", short)
		}
		if card.Reading(false) == card.Reading(true) {
			t.Errorf("%s 的正位与逆位解读相同", short)
		}

		if card.Suit == SuitMajor {
			majors++
			if card.Rank != 0 {
				t.Errorf("%s 是大阿尔卡纳，牌位应为 0，实际 %d", short, card.Rank)
			}
			// 大阿尔卡纳必须有逐张撰写的正文，且不能是关键词的复制。
			if card.ReadingUp == "" || card.ReadingRev == "" {
				t.Errorf("%s 缺少大阿尔卡纳正文", short)
			}
			if len([]rune(card.ReadingUp)) < 20 {
				t.Errorf("%s 正位解读只有 %d 字，疑似未完成", short, len([]rune(card.ReadingUp)))
			}
			continue
		}

		if card.Rank < 1 || card.Rank > 14 {
			t.Errorf("%s 的牌位应在 1-14，实际 %d", short, card.Rank)
		}
		ranks[card.Rank]++

		// 小阿尔卡纳的解读由牌组领域与牌位阶段组合而成。
		if card.ReadingUp != "" || card.ReadingRev != "" {
			t.Errorf("%s 是小阿尔卡纳，不应有逐张正文", short)
		}
		if !strings.Contains(card.Reading(false), card.Suit.String()) {
			t.Errorf("%s 的解读未提及牌组: %q", short, card.Reading(false))
		}
		if !strings.Contains(card.Reading(true), "逆位") {
			t.Errorf("%s 的逆位解读未标注朝向: %q", short, card.Reading(true))
		}
	}

	if majors != 22 {
		t.Errorf("大阿尔卡纳应有 22 张，实际 %d", majors)
	}
	if len(ranks) != 14 {
		t.Errorf("小阿尔卡纳应覆盖 14 个牌位，实际 %d", len(ranks))
	}
	for rank, n := range ranks {
		if n != 4 {
			t.Errorf("牌位 %d 应有 4 张（四种花色），实际 %d", rank, n)
		}
	}
}

// TestTarotReadingComposition 确认小阿尔卡纳的解读确实把牌组与牌位拼了出来。
func TestTarotReadingComposition(t *testing.T) {
	card := tarotDeck["wa03"]
	if card == nil {
		t.Fatal("wa03 不存在")
	}

	up := card.Reading(false)
	for _, want := range []string{"权杖", suitDomain[SuitWands], "正位", rankStages[2].up} {
		if !strings.Contains(up, want) {
			t.Errorf("正位解读缺少 %q: %s", want, up)
		}
	}
	// 关键词由「牌意」单独展示，解读正文不应重复。
	if strings.Contains(up, card.MeaningUp) {
		t.Errorf("正位解读不应重复关键词 %q: %s", card.MeaningUp, up)
	}

	rev := card.Reading(true)
	for _, want := range []string{"逆位", rankStages[2].rev} {
		if !strings.Contains(rev, want) {
			t.Errorf("逆位解读缺少 %q: %s", want, rev)
		}
	}
	if strings.Contains(rev, card.MeaningRev) {
		t.Errorf("逆位解读不应重复关键词 %q: %s", card.MeaningRev, rev)
	}
}

// TestTarotEnglishNames 英文牌名应为纯英文，花色用英文拼写而非中文。
func TestTarotEnglishNames(t *testing.T) {
	want := map[string]string{
		"ar00": "The Fool",
		"wa01": "Ace of Wands",
		"cu07": "Seven of Cups",
		"sw10": "Ten of Swords",
		"pe14": "King of Pentacles",
	}
	for short, name := range want {
		card := tarotDeck[short]
		if card == nil {
			t.Errorf("%s 不存在", short)
			continue
		}
		if card.NameEN != name {
			t.Errorf("%s 的英文名应为 %q，实际 %q", short, name, card.NameEN)
		}
	}

	for short, card := range tarotDeck {
		for _, r := range card.NameEN {
			if r > unicode.MaxASCII {
				t.Errorf("%s 的英文名混入了非 ASCII 字符: %q", short, card.NameEN)
				break
			}
		}
	}
}

// TestTarotReadingRankFallback 牌位数据异常时退回关键词，不应 panic。
func TestTarotReadingRankFallback(t *testing.T) {
	card := &TarotCard{
		NameShort: "xx99", NameCN: "测试牌", Suit: SuitWands, Rank: 0,
		MeaningUp: "关键词正", MeaningRev: "关键词逆",
	}
	if got := card.Reading(false); got != "关键词正" {
		t.Errorf("越界牌位应退回正位关键词，实际 %q", got)
	}
	if got := card.Reading(true); got != "关键词逆" {
		t.Errorf("越界牌位应退回逆位关键词，实际 %q", got)
	}
}

// TestTarotPositions 确认牌阵位置定义。
func TestTarotPositions(t *testing.T) {
	if got := tarotPositionsFor(1); len(got) != 1 {
		t.Errorf("1 张牌阵应有 1 个位置，实际 %d", len(got))
	}

	three := tarotPositionsFor(3)
	if len(three) != 3 {
		t.Fatalf("3 张牌阵应有 3 个位置，实际 %d", len(three))
	}
	for i, want := range []string{"过去", "现在", "未来"} {
		if three[i].Name != want {
			t.Errorf("位置 %d 应为 %q，实际 %q", i, want, three[i].Name)
		}
		if three[i].Focus == "" {
			t.Errorf("位置 %q 缺少说明", three[i].Name)
		}
	}

	if tarotPositionsFor(2) != nil {
		t.Error("未定义的张数应返回 nil")
	}
}

// TestPositionName 位置名仅在多张牌阵中出现，越界返回空串。
func TestPositionName(t *testing.T) {
	if got := positionName(tarotPositionsFor(1), 0); got != "" {
		t.Errorf("单张牌阵不应有位置名，实际 %q", got)
	}
	three := tarotPositionsFor(3)
	if got := positionName(three, 1); got != "现在" {
		t.Errorf("第 2 张位置应为「现在」，实际 %q", got)
	}
	for _, i := range []int{-1, 3, 9} {
		if got := positionName(three, i); got != "" {
			t.Errorf("越界下标 %d 应返回空串，实际 %q", i, got)
		}
	}
	if got := positionName(nil, 0); got != "" {
		t.Errorf("无位置定义时应返回空串，实际 %q", got)
	}
}

// TestRenderTarotSpread 三张牌阵的合成图宽度应为三列加间隔。
func TestRenderTarotSpread(t *testing.T) {
	shorts := []string{"ar10", "wa01", "cu07"}
	cols := make([]tarotColumn, len(shorts))
	for i, short := range shorts {
		cols[i] = tarotColumn{
			Reading:  TarotReading{Card: *tarotDeck[short], IsReverse: i%2 == 1},
			Face:     decodeAsset(tarotAssetPath(short)),
			Position: positionName(tarotPositionsFor(3), i),
		}
	}

	data, err := renderTarotSpread(cols)
	if err != nil {
		t.Fatalf("牌阵渲染失败: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("输出不是有效图片: %v", err)
	}
	want := tarotCardWidth*len(cols) + tarotSpreadGap*(len(cols)-1)
	if img.Bounds().Dx() != want {
		t.Errorf("牌阵图宽度 = %d, 期望 %d", img.Bounds().Dx(), want)
	}
	if img.Bounds().Dy() < 100 {
		t.Errorf("牌阵图高度异常: %d", img.Bounds().Dy())
	}
}

// TestRenderTarotSpreadSingle 单张牌阵应与单张卡片同宽。
func TestRenderTarotSpreadSingle(t *testing.T) {
	reading := TarotReading{Card: *tarotDeck["ar18"], IsReverse: true}
	data, err := renderTarotSpread([]tarotColumn{{
		Reading: reading,
		Face:    decodeAsset(tarotAssetPath(reading.Card.NameShort)),
	}})
	if err != nil {
		t.Fatalf("单张牌阵渲染失败: %v", err)
	}
	assertCardImage(t, data, tarotCardWidth)
}

// TestRenderTarotSpreadNoFaces 牌面素材缺失时仍应输出纯文字牌阵。
func TestRenderTarotSpreadNoFaces(t *testing.T) {
	cols := make([]tarotColumn, 3)
	for i, short := range []string{"ar01", "sw05", "pe11"} {
		cols[i] = tarotColumn{
			Reading:  TarotReading{Card: *tarotDeck[short], IsReverse: i == 2},
			Position: positionName(tarotPositionsFor(3), i),
		}
	}
	data, err := renderTarotSpread(cols)
	if err != nil {
		t.Fatalf("缺牌面时渲染失败: %v", err)
	}
	assertCardImage(t, data, tarotCardWidth*3+tarotSpreadGap*2)
}

// TestRenderTarotSpreadEmpty 空牌阵必须报错，交由调用方降级。
func TestRenderTarotSpreadEmpty(t *testing.T) {
	if _, err := renderTarotSpread(nil); err == nil {
		t.Error("空牌阵应返回错误")
	}
}

// TestFormatTarotMulti 三张牌阵的两种排版都应包含位置、牌意、解读与固定说明。
func TestFormatTarotMulti(t *testing.T) {
	readings := []TarotReading{
		{Card: *tarotDeck["ar10"], IsReverse: false},
		{Card: *tarotDeck["wa01"], IsReverse: true},
		{Card: *tarotDeck["cu07"], IsReverse: false},
	}

	md := formatTarotMD(readings)
	for _, want := range []string{
		"🃏 塔罗牌阵 · 过去 · 现在 · 未来",
		"① 过去", "② 现在", "③ 未来",
		"命运之轮", "权杖一", "圣杯七",
		"（正位）", "（逆位）",
		"**牌意**", "**解读**",
		tarotSource, tarotReverseHint, tarotDisclaimer,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown 输出缺少 %q\n%s", want, md)
		}
	}

	txt := formatTarotText(readings)
	for _, want := range []string{
		"🃏 塔罗牌阵 · 过去 · 现在 · 未来",
		"① 过去", "② 现在", "③ 未来",
		"牌意：", "解读：",
		tarotSource, tarotReverseHint, tarotDisclaimer,
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("纯文本输出缺少 %q\n%s", want, txt)
		}
	}
	for _, bad := range []string{"**", "---"} {
		if strings.Contains(txt, bad) {
			t.Errorf("纯文本输出不应包含 Markdown 标记 %q", bad)
		}
	}
}

// TestFormatTarotSingle 单张牌不显示序号与牌阵位置。
func TestFormatTarotSingle(t *testing.T) {
	readings := []TarotReading{{Card: *tarotDeck["ar18"], IsReverse: false}}

	md := formatTarotMD(readings)
	if !strings.Contains(md, "🃏 塔罗牌 · 月亮 · 正位") {
		t.Errorf("单张标题不符:\n%s", md)
	}
	for _, bad := range []string{"①", "当前", "过去"} {
		if strings.Contains(md, bad) {
			t.Errorf("单张牌不应出现 %q\n%s", bad, md)
		}
	}

	txt := formatTarotText(readings)
	if !strings.Contains(txt, "🃏 塔罗牌 · 月亮 · 正位") {
		t.Errorf("单张纯文本标题不符:\n%s", txt)
	}
	if !strings.Contains(txt, "牌意：") || !strings.Contains(txt, "解读：") {
		t.Errorf("单张纯文本缺少牌意或解读:\n%s", txt)
	}
}

// TestTarotFooterReverseHint 逆位提示只在出现逆位时附加。
func TestTarotFooterReverseHint(t *testing.T) {
	upright := []TarotReading{{Card: *tarotDeck["ar19"], IsReverse: false}}
	reversed := []TarotReading{
		{Card: *tarotDeck["ar19"], IsReverse: false},
		{Card: *tarotDeck["sw05"], IsReverse: true},
	}

	if strings.Contains(tarotFooter(upright), tarotReverseHint) {
		t.Error("全正位时不应附加逆位提示")
	}
	if !strings.Contains(tarotFooter(reversed), tarotReverseHint) {
		t.Error("出现逆位时应附加逆位提示")
	}
	for _, footer := range []string{tarotFooter(upright), tarotFooter(reversed)} {
		if !strings.Contains(footer, tarotSource) || !strings.Contains(footer, tarotDisclaimer) {
			t.Errorf("固定说明应始终包含出处与免责声明: %s", footer)
		}
	}
}

// TestDrawTarotDistinct 抽牌互不重复，且数量受牌库上限约束。
func TestDrawTarotDistinct(t *testing.T) {
	for range 50 {
		readings := drawTarot(3)
		if len(readings) != 3 {
			t.Fatalf("应抽 3 张，实际 %d", len(readings))
		}
		seen := make(map[string]bool, len(readings))
		for _, r := range readings {
			if seen[r.Card.NameShort] {
				t.Fatalf("抽到重复的牌: %s", r.Card.NameShort)
			}
			seen[r.Card.NameShort] = true
		}
	}

	if got := len(drawTarot(200)); got != 78 {
		t.Errorf("超过牌库总数时应返回全部 78 张，实际 %d", got)
	}
}
