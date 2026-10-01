package wordle

import (
	"slices"
	"testing"
)

func TestLoadWordBank_AllSupportedLengths(t *testing.T) {
	for _, l := range SupportedLengths {
		bank, err := loadWordBank(l)
		if err != nil {
			t.Fatalf("加载 %d 字母词库失败: %v", l, err)
		}
		if bank.Length != l {
			t.Errorf("bank.Length = %d, 期望 %d", bank.Length, l)
		}
		if bank.Size() == 0 {
			t.Fatalf("%d 字母词库答案池为空", l)
		}
		for _, w := range bank.answers {
			if len(w) != l {
				t.Fatalf("答案 %q 长度不是 %d", w, l)
			}
			if !bank.IsAllowed(w) {
				t.Fatalf("答案 %q 不在合法输入集合中", w)
			}
		}
	}
}

func TestLoadWordBank_UnsupportedLength(t *testing.T) {
	if _, err := loadWordBank(3); err == nil {
		t.Fatal("长度 3 没有词库，应返回错误")
	}
}

// TestWordBank_AnswersExcludeBlocklist 验证谜底池与冷门池都不含屏蔽词
// （人名、地名、月份/星期、粗俗词），避免默认对局抽到人名或专有名词。
func TestWordBank_AnswersExcludeBlocklist(t *testing.T) {
	excluded, err := readWordSet("words/excluded.txt")
	if err != nil {
		t.Fatalf("读取 excluded.txt 失败: %v", err)
	}
	if len(excluded) == 0 {
		t.Fatal("excluded.txt 不应为空")
	}
	for _, l := range SupportedLengths {
		bank, err := loadWordBank(l)
		if err != nil {
			t.Fatalf("加载 %d 字母词库失败: %v", l, err)
		}
		for _, w := range bank.answers {
			if _, bad := excluded[w]; bad {
				t.Errorf("%d 字母谜底池含屏蔽词 %q", l, w)
			}
		}
		for _, w := range bank.obscure {
			if _, bad := excluded[w]; bad {
				t.Errorf("%d 字母冷门池含屏蔽词 %q", l, w)
			}
		}
	}
}

// TestWordBank_BlockedWordStillGuessable 验证被屏蔽的词仍可作为合法猜测，
// 只是不会成为谜底——既能防止出题抽到人名，又不影响玩家输入。
func TestWordBank_BlockedWordStillGuessable(t *testing.T) {
	cases := map[int][]string{
		4: {"anna", "asia", "kent"},
		5: {"alice", "james", "china"},
		6: {"jordan", "amelia"},
		7: {"arizona", "america", "abigail"},
	}
	for _, l := range SupportedLengths {
		bank, err := loadWordBank(l)
		if err != nil {
			t.Fatalf("加载 %d 字母词库失败: %v", l, err)
		}
		for _, w := range cases[l] {
			if !bank.IsAllowed(w) {
				t.Errorf("%q 应仍可作为合法猜测", w)
			}
			if slices.Contains(bank.answers, w) {
				t.Errorf("%q 不应成为谜底", w)
			}
		}
	}
}

// TestWordBank_KeepsWordsThatLookLikeNames 防止误伤：像 winter/west/fancy/fairy 这类
// "偶尔被用作名字"的常见词应当保留在谜底池中。
func TestWordBank_KeepsWordsThatLookLikeNames(t *testing.T) {
	cases := map[int][]string{
		4: {"west"},
		5: {"fancy", "fairy"},
		6: {"winter", "autumn", "spring", "summer"},
	}
	for _, l := range SupportedLengths {
		bank, err := loadWordBank(l)
		if err != nil {
			t.Fatalf("加载 %d 字母词库失败: %v", l, err)
		}
		for _, w := range cases[l] {
			if !slices.Contains(bank.answers, w) {
				t.Errorf("%q 是常见词，应保留在 %d 字母谜底池中", w, l)
			}
		}
	}
}

func TestWordBank_IsAllowed(t *testing.T) {
	bank, err := loadWordBank(5)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if !bank.IsAllowed("crane") {
		t.Error("crane 应在词库中")
	}
	if bank.IsAllowed("zzzzz") {
		t.Error("zzzzz 不应在词库中")
	}
	if bank.IsAllowed("CRANE") {
		t.Error("词库匹配应区分大小写（调用方负责小写化）")
	}
}

func TestWordBank_DailyIndexStable(t *testing.T) {
	bank, err := loadWordBank(5)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	a := bank.DailyIndex("2026-10-01")
	b := bank.DailyIndex("2026-10-01")
	if a != b {
		t.Fatalf("同一日期应得到相同下标: %d != %d", a, b)
	}
	if a < 0 || a >= bank.Size() {
		t.Fatalf("下标越界: %d", a)
	}
	if w := bank.AnswerAt(a); len(w) != 5 {
		t.Fatalf("AnswerAt 返回非法单词 %q", w)
	}
	if bank.DailyIndex("2026-10-02") == a {
		t.Log("相邻两天恰好同词（概率事件，非错误）")
	}
}

func TestWordBank_PickInRange(t *testing.T) {
	bank, err := loadWordBank(4)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	for range 50 {
		if w := bank.Pick(); len(w) != 4 || !bank.IsAllowed(w) {
			t.Fatalf("Pick 返回非法单词 %q", w)
		}
	}
}

func TestIsSupportedLength(t *testing.T) {
	for _, l := range SupportedLengths {
		if !IsSupportedLength(l) {
			t.Errorf("%d 应被支持", l)
		}
	}
	if IsSupportedLength(3) || IsSupportedLength(8) {
		t.Error("3/8 不应被支持")
	}
}

func TestIsAlpha(t *testing.T) {
	if !isAlpha("crane") {
		t.Error("crane 应为纯字母")
	}
	for _, s := range []string{"", "cran3", "cr ne", "Crane"} {
		if isAlpha(s) {
			t.Errorf("%q 不应判定为纯小写字母", s)
		}
	}
}

func TestPickAnswers_MultiDistinct(t *testing.T) {
	bank, err := loadWordBank(5)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	for range 50 {
		got := pickAnswers(bank, 0, 0, 4)
		if len(got) != 4 {
			t.Fatalf("应返回 4 个谜底，实际 %d (%v)", len(got), got)
		}
		seen := make(map[string]struct{}, 4)
		for _, w := range got {
			if !bank.IsAllowed(w) {
				t.Fatalf("谜底 %q 不在词库中", w)
			}
			if _, dup := seen[w]; dup {
				t.Fatalf("多谜底不应重复: %v", got)
			}
			seen[w] = struct{}{}
		}
	}
}

func TestPickAnswers_ObscurePool(t *testing.T) {
	bank, err := loadWordBank(5)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if bank.ObscureSize() == 0 {
		t.Fatal("冷门词库不应为空")
	}
	common := make(map[string]struct{}, bank.Size())
	for _, w := range bank.answers {
		common[w] = struct{}{}
	}
	for range 50 {
		got := pickAnswers(bank, RuleObscure, 0, 2)
		if len(got) != 2 {
			t.Fatalf("应返回 2 个谜底，实际 %d (%v)", len(got), got)
		}
		for _, w := range got {
			if _, isCommon := common[w]; isCommon {
				t.Fatalf("冷门模式不应抽到常用谜底 %q", w)
			}
		}
	}
}

// TestWordBank_ObscurePoolConsistency 验证冷门池是合法输入的子集且与常用谜底不相交，
// 保证冷门局的谜底同样能被猜出（否则玩家永远无法答对）。
func TestWordBank_ObscurePoolConsistency(t *testing.T) {
	for _, l := range SupportedLengths {
		bank, err := loadWordBank(l)
		if err != nil {
			t.Fatalf("加载 %d 字母词库失败: %v", l, err)
		}
		if bank.ObscureSize() == 0 {
			t.Fatalf("%d 字母冷门池不应为空", l)
		}
		answers := make(map[string]struct{}, bank.Size())
		for _, w := range bank.answers {
			answers[w] = struct{}{}
		}
		for _, w := range bank.obscure {
			if len(w) != l {
				t.Errorf("冷门词 %q 长度不是 %d", w, l)
			}
			if !bank.IsAllowed(w) {
				t.Errorf("冷门词 %q 不在合法输入集合中，将无法被猜出", w)
			}
			if _, dup := answers[w]; dup {
				t.Errorf("冷门词 %q 不应同时是常用谜底", w)
			}
		}
	}
}

func TestPickDailyAnswers_StableDistinct(t *testing.T) {
	bank, err := loadWordBank(5)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	a := pickDailyAnswers(bank, 0, 4, "2026-10-01")
	b := pickDailyAnswers(bank, 0, 4, "2026-10-01")
	if len(a) != 4 {
		t.Fatalf("应返回 4 个谜底，实际 %d (%v)", len(a), a)
	}
	seen := make(map[string]struct{}, 4)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("同一日期应稳定: %v != %v", a, b)
		}
		if _, dup := seen[a[i]]; dup {
			t.Fatalf("每日多谜底不应重复: %v", a)
		}
		seen[a[i]] = struct{}{}
	}
}
