package wordle

import (
	"reflect"
	"testing"
)

func TestStyleFromMark(t *testing.T) {
	cases := map[Mark]TileStyle{
		Absent:  StyleAbsent,
		Present: StylePresent,
		Correct: StyleCorrect,
	}
	for in, want := range cases {
		if got := styleFromMark(in); got != want {
			t.Errorf("styleFromMark(%v) = %v, 期望 %v", in, got, want)
		}
	}
}

func TestApplyNear(t *testing.T) {
	word := []rune("xxrxx")
	styles := []TileStyle{StyleAbsent, StyleAbsent, StylePresent, StyleAbsent, StyleAbsent}
	// answer 中的 'r' 在下标 1，猜测中的 'r' 在下标 2，相差 1 -> 升级为青。
	applyNear(styles, word, "crane")
	if styles[2] != StyleNear {
		t.Fatalf("相邻命中应升级为 StyleNear，实际 %v", styles[2])
	}

	// 距离大于 1 时保持黄色。
	far := []rune("xxxrx")
	styles2 := []TileStyle{StyleAbsent, StyleAbsent, StyleAbsent, StylePresent, StyleAbsent}
	applyNear(styles2, far, "crane")
	if styles2[3] != StylePresent {
		t.Fatalf("非相邻命中应保持 StylePresent，实际 %v", styles2[3])
	}
}

func TestApplyRepeat(t *testing.T) {
	styles := []TileStyle{StyleAbsent, StyleAbsent, StyleAbsent, StyleAbsent, StylePresent}
	applyRepeat(styles, []rune("xxxxe"), "eaten") // eaten 含两个 e
	if styles[4] != StyleRepeat {
		t.Fatalf("重复字母应升级为 StyleRepeat，实际 %v", styles[4])
	}
	// 单个 e 的谜底不应变紫。
	styles2 := []TileStyle{StylePresent, StyleAbsent, StyleAbsent, StyleAbsent, StyleAbsent}
	applyRepeat(styles2, []rune("exxxx"), "crane")
	if styles2[0] != StylePresent {
		t.Fatalf("非重复字母应保持 StylePresent，实际 %v", styles2[0])
	}
}

func TestSwapCorrectPresent(t *testing.T) {
	styles := []TileStyle{StyleCorrect, StylePresent, StyleAbsent}
	swapCorrectPresent(styles)
	if styles[0] != StylePresent || styles[1] != StyleCorrect || styles[2] != StyleAbsent {
		t.Fatalf("语义互换错误: %v", styles)
	}
}

func TestInvertStyles(t *testing.T) {
	styles := []TileStyle{StyleCorrect, StylePresent, StyleAbsent}
	invertStyles(styles)
	if styles[0] != StyleAbsent || styles[2] != StyleCorrect || styles[1] != StylePresent {
		t.Fatalf("反转错误: %v", styles)
	}
}

func TestHitOnly(t *testing.T) {
	styles := []TileStyle{StyleCorrect, StylePresent, StyleAbsent}
	hitOnly(styles)
	if styles[0] != StylePresent || styles[1] != StylePresent || styles[2] != StyleAbsent {
		t.Fatalf("只留命中错误: %v", styles)
	}
}

func TestApplyBlind(t *testing.T) {
	styles := []TileStyle{StyleCorrect, StylePresent, StyleAbsent}
	applyBlind(styles)
	if styles[0] != StyleCorrect || styles[1] != StyleAbsent || styles[2] != StyleAbsent {
		t.Fatalf("盲猜应只把黄色降级为灰色，实际 %v", styles)
	}
}

// TestDisplayStyles_BlindKeepsTrueMarks 验证盲猜只改写显示样式，
// 不改写逻辑真值 Marks（胜负判定与困难模式校验都依赖真值）。
func TestDisplayStyles_BlindKeepsTrueMarks(t *testing.T) {
	marks := Evaluate("crane", "eaten")
	g := newDisplayGame(RuleBlind, Guess{Word: "eaten", Marks: marks})
	got := displayStyles(g)[0][0]
	for i, s := range got {
		if marks[i] == Present && s != StyleAbsent {
			t.Fatalf("位置 %d 的黄色应显示为灰，实际 %v", i, s)
		}
	}
	if marks[0] != Present {
		t.Fatal("displayStyles 不应改写真值 Marks")
	}
}

func TestHashedIndices(t *testing.T) {
	a := hashedIndices("seed", 3, 10)
	b := hashedIndices("seed", 3, 10)
	if len(a) != 3 {
		t.Fatalf("应返回 3 个下标，实际 %v", a)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("同一 seed 应稳定: %v != %v", a, b)
	}
	seen := map[int]bool{}
	for _, i := range a {
		if i < 0 || i >= 10 {
			t.Fatalf("下标越界: %v", a)
		}
		if seen[i] {
			t.Fatalf("下标应互不相同: %v", a)
		}
		seen[i] = true
	}
	if got := hashedIndices("s", 99, 4); len(got) != 4 {
		t.Fatalf("n 超过总数时应 clamp 到 4，实际 %v", got)
	}
}

func TestApplyDecoyAndGlitchDeterministic(t *testing.T) {
	base := []TileStyle{StyleAbsent, StyleAbsent, StyleAbsent, StyleAbsent, StyleAbsent}
	a := append([]TileStyle(nil), base...)
	b := append([]TileStyle(nil), base...)
	applyDecoy(a, 2, "seed")
	applyDecoy(b, 2, "seed")
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("诱饵应确定性: %v != %v", a, b)
	}
	colored := 0
	for _, s := range a {
		if s == StylePresent {
			colored++
		}
	}
	if colored != 2 {
		t.Fatalf("诱饵应恰好谎报 2 格，实际 %v", a)
	}

	g1 := append([]TileStyle(nil), base...)
	g2 := append([]TileStyle(nil), base...)
	applyGlitch(g1, "seed")
	applyGlitch(g2, "seed")
	if !reflect.DeepEqual(g1, g2) {
		t.Fatalf("故障应确定性: %v != %v", g1, g2)
	}
}

func TestApplyColorFog(t *testing.T) {
	styles := []TileStyle{StyleCorrect, StylePresent, StylePresent, StyleAbsent, StyleCorrect}
	applyColorFog(styles, 2, "seed")
	colored := 0
	for _, s := range styles {
		if s != StyleAbsent {
			colored++
		}
	}
	if colored != 2 {
		t.Fatalf("颜色预算应只保留 2 格，实际 %v", styles)
	}
	// 绿色优先保留：两个 Correct 都应留下。
	if styles[0] != StyleCorrect || styles[4] != StyleCorrect {
		t.Fatalf("应优先保留价值更高的绿色: %v", styles)
	}
}

func TestApplyDecayUnknown(t *testing.T) {
	styles := []TileStyle{StyleCorrect, StylePresent, StyleAbsent}
	applyDecay(styles)
	for i, s := range styles {
		if s != StyleAbsent {
			t.Fatalf("衰减后应全为灰，位置 %d = %v", i, s)
		}
	}
	styles2 := []TileStyle{StyleCorrect, StylePresent, StyleAbsent}
	applyUnknown(styles2)
	for i, s := range styles2 {
		if s != StyleUnknown {
			t.Fatalf("未知格应全为 StyleUnknown，位置 %d = %v", i, s)
		}
	}
}

func TestScoreColor(t *testing.T) {
	if scoreColor(0, 5) != colAbsent {
		t.Errorf("0 绿应映射为灰，实际 %v", scoreColor(0, 5))
	}
	if scoreColor(5, 5) != colCorrect {
		t.Errorf("全绿应映射为绿，实际 %v", scoreColor(5, 5))
	}
	mid := scoreColor(3, 5)
	if mid == colAbsent || mid == colCorrect {
		t.Errorf("中间值应是渐变，实际 %v", mid)
	}
}

func newDisplayGame(rules RuleSet, guesses ...Guess) *Game {
	return &Game{
		ID:          "g1",
		Length:      5,
		MaxAttempts: 6,
		Answers:     []string{"crane"},
		Solved:      []bool{false},
		Rules:       rules,
		Guesses:     guesses,
	}
}

func TestDisplayStyles_InvertAndDeterministic(t *testing.T) {
	g := newDisplayGame(RuleInvert, Guess{Word: "crate", Marks: Evaluate("crane", "crate")})
	got := displayStyles(g)
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("形状错误: %v", got)
	}
	// 真值 c,c,c,absent,c 反转后应为 灰,灰,灰,绿,灰。
	want := []TileStyle{StyleAbsent, StyleAbsent, StyleAbsent, StyleCorrect, StyleAbsent}
	if !reflect.DeepEqual(got[0][0], want) {
		t.Fatalf("反转显示 = %v, 期望 %v", got[0][0], want)
	}
	if !reflect.DeepEqual(got, displayStyles(g)) {
		t.Fatal("displayStyles 应确定性")
	}
}

func TestDisplayStyles_MoleAndScore(t *testing.T) {
	g := newDisplayGame(RuleMole, Guess{Word: "crate", Marks: Evaluate("crane", "crate")})
	styles := displayStyles(g)[0][0]
	want := hashedIndices("mole:"+g.ID, 1, g.Length)[0]
	if styles[want] != StyleCorrect {
		t.Fatalf("内鬼位置 %d 应显示为绿，实际 %v", want, styles)
	}

	g2 := newDisplayGame(RuleScoreColor, Guess{Word: "crate", Marks: Evaluate("crane", "crate")})
	for _, s := range displayStyles(g2)[0][0] {
		if s != StyleScore {
			t.Fatalf("计分色应整行同色，实际 %v", displayStyles(g2)[0][0])
		}
	}
}

func TestDisplayStyles_DecayAndDelayed(t *testing.T) {
	guesses := []Guess{
		{Word: "crate", Marks: Evaluate("crane", "crate")},
		{Word: "eaten", Marks: Evaluate("crane", "eaten")},
	}
	decay := newDisplayGame(RuleDecay, guesses...)
	decay.DecayRows = 1
	styles := displayStyles(decay)
	for _, s := range styles[0][0] {
		if s != StyleAbsent {
			t.Fatalf("衰减应把旧行变灰，实际 %v", styles[0][0])
		}
	}
	hasColor := false
	for _, s := range styles[1][0] {
		if s != StyleAbsent {
			hasColor = true
		}
	}
	if !hasColor {
		t.Fatalf("最近一行应保留颜色，实际 %v", styles[1][0])
	}

	delayed := newDisplayGame(RuleDelayed, guesses...)
	delayed.DelayedRows = 1
	dstyles := displayStyles(delayed)
	for _, s := range dstyles[1][0] {
		if s != StyleUnknown {
			t.Fatalf("延迟应隐藏最新行，实际 %v", dstyles[1][0])
		}
	}
}

func TestKeyStyles(t *testing.T) {
	g := newDisplayGame(RuleHiddenKey, Guess{Word: "crate", Marks: Evaluate("crane", "crate")})
	styles := displayStyles(g)
	keys := keyStyles(g, styles)
	if keys['c'] != StyleCorrect {
		t.Fatalf("键盘 'c' 应为 Correct，实际 %v", keys['c'])
	}
	if _, ok := keys['z']; ok {
		t.Fatal("未出现的字母不应进入键盘状态")
	}
}

func TestKeyStylesVisible_HidesFoggedRows(t *testing.T) {
	guesses := []Guess{
		{Word: "crate", Marks: Evaluate("crane", "crate")},
		{Word: "xxxxx", Marks: Evaluate("crane", "xxxxx")},
	}
	g := newDisplayGame(0, guesses...)
	v := gameBoardView(g)
	v.FogRows = 1
	v.Styles = displayStyles(g)
	keys := keyStylesVisible(v, v.Styles)
	for _, ch := range "crate" {
		if _, ok := keys[ch]; ok {
			t.Fatalf("被迷雾遮住的行不应进入键盘状态，泄漏字母 %q: %v", ch, keys)
		}
	}
	if _, ok := keys['x']; !ok {
		t.Fatalf("可见行应进入键盘状态，实际 %v", keys)
	}
}
