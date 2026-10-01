package wordle

import "testing"

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
