package wordle

import "testing"

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		guess  string
		want   []Mark
	}{
		{
			name:   "所有字母全中",
			answer: "crane",
			guess:  "crane",
			want:   []Mark{Correct, Correct, Correct, Correct, Correct},
		},
		{
			name:   "位置正确优先于存在",
			answer: "crane",
			guess:  "crate",
			want:   []Mark{Correct, Correct, Correct, Absent, Correct},
		},
		{
			name:   "存在但位置不对",
			answer: "crane",
			guess:  "eaten",
			want:   []Mark{Present, Present, Absent, Absent, Present},
		},
		{
			name:   "重复字母计数不超量",
			answer: "abbey",
			guess:  "bobby",
			want:   []Mark{Present, Absent, Correct, Absent, Correct},
		},
		{
			name:   "长度不一致返回全 Absent",
			answer: "crane",
			guess:  "cranee",
			want:   []Mark{Absent, Absent, Absent, Absent, Absent, Absent},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.answer, tc.guess)
			if len(got) != len(tc.want) {
				t.Fatalf("长度 = %d, 期望 %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("位置 %d = %v, 期望 %v（完整 %v）", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

func TestEvaluate_CaseInsensitive(t *testing.T) {
	got := Evaluate("CRANE", "Crane")
	for i, m := range got {
		if m != Correct {
			t.Fatalf("大小写不敏感失败，位置 %d = %v", i, m)
		}
	}
}

func TestGuessSolved(t *testing.T) {
	if !(Guess{Word: "crane", Marks: []Mark{Correct, Correct, Correct, Correct, Correct}}).Solved() {
		t.Fatal("全绿应当判定为猜中")
	}
	if (Guess{Word: "crane", Marks: []Mark{Correct, Present, Correct, Correct, Correct}}).Solved() {
		t.Fatal("含黄色不应判定为猜中")
	}
	if (Guess{}).Solved() {
		t.Fatal("空猜测不应判定为猜中")
	}
}

func TestKeyStates(t *testing.T) {
	guesses := []Guess{
		{Word: "crane", Marks: []Mark{Absent, Present, Correct, Absent, Absent}},
		{Word: "crest", Marks: []Mark{Correct, Absent, Absent, Absent, Absent}},
	}
	got := KeyStates(guesses, -1)
	if got['c'] != Correct {
		t.Errorf("'c' 应升级为 Correct，实际 %v", got['c'])
	}
	if got['r'] != Present {
		t.Errorf("'r' 应为 Present，实际 %v", got['r'])
	}
	if got['a'] != Correct {
		t.Errorf("'a' 应为 Correct，实际 %v", got['a'])
	}
	if _, ok := got['z']; ok {
		t.Errorf("未出现的字母不应出现在键盘状态中")
	}
}

func TestKeyStates_MultiBoard(t *testing.T) {
	guesses := []Guess{
		{
			Word:  "crate",
			Marks: []Mark{Absent, Absent, Absent, Absent, Absent},
			Boards: [][]Mark{
				{Absent, Absent, Absent, Absent, Absent},
				{Correct, Absent, Absent, Absent, Absent},
			},
		},
	}
	// board = -1 汇总全部棋盘：c 在 board 1 为绿色。
	if got := KeyStates(guesses, -1); got['c'] != Correct {
		t.Errorf("汇总模式应取所有棋盘的最佳状态，实际 %v", got['c'])
	}
	// 只看 board 0 时 c 仍为灰色。
	if got := KeyStates(guesses, 0); got['c'] != Absent {
		t.Errorf("board 0 的 c 应为灰，实际 %v", got['c'])
	}
}

func TestGuessMarksFor(t *testing.T) {
	g := Guess{
		Word:  "crate",
		Marks: []Mark{Correct, Absent, Absent, Absent, Absent},
		Boards: [][]Mark{
			{Correct, Absent, Absent, Absent, Absent},
			{Absent, Present, Absent, Absent, Absent},
		},
	}
	if m := g.MarksFor(0); m[0] != Correct {
		t.Errorf("MarksFor(0) 错误: %v", m)
	}
	if m := g.MarksFor(1); m[1] != Present {
		t.Errorf("MarksFor(1) 错误: %v", m)
	}
	if m := g.MarksFor(9); m != nil {
		t.Errorf("越界应返回 nil，实际 %v", m)
	}
	// 单谜底（Boards 为空）时 MarksFor(0) 回退到 Marks。
	one := Guess{Word: "crane", Marks: []Mark{Correct}}
	if m := one.MarksFor(0); len(m) != 1 || m[0] != Correct {
		t.Errorf("单谜底回退失败: %v", m)
	}
}
