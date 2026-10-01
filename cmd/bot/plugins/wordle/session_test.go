package wordle

import (
	"strings"
	"testing"
	"time"
)

func TestParseScope(t *testing.T) {
	cases := map[string]Scope{
		"group":    ScopeGroup,
		"user":     ScopeUser,
		"GROUP":    ScopeGroup,
		" private": ScopeUser,
	}
	for in, want := range cases {
		got, ok := ParseScope(in)
		if !ok || got != want {
			t.Errorf("ParseScope(%q) = %v,%v; 期望 %v,true", in, got, ok, want)
		}
	}
	if _, ok := ParseScope("guild"); ok {
		t.Error("guild 不是合法维度")
	}
}

func TestSessionKey(t *testing.T) {
	group := SessionKey("qq", "g1", "u1", ScopeGroup)
	other := SessionKey("qq", "g1", "u2", ScopeGroup)
	if group != other {
		t.Error("群维度会话键不应包含用户 ID")
	}
	user1 := SessionKey("qq", "g1", "u1", ScopeUser)
	user2 := SessionKey("qq", "g1", "u2", ScopeUser)
	if user1 == user2 {
		t.Error("用户维度会话键应彼此隔离")
	}
	if strings.Contains(user1, "|") == false {
		t.Error("会话键应包含分隔符")
	}
}

func TestSessionStore_FindPrefersUserThenGroup(t *testing.T) {
	s := NewSessionStore(time.Minute)
	groupKey := SessionKey("qq", "g1", "", ScopeGroup)
	userKey := SessionKey("qq", "g1", "u1", ScopeUser)

	s.Put(groupKey, &Game{Length: 5, UpdatedAt: time.Now()})
	if g, ok := s.Find("qq", "g1", "u1"); !ok || g.Length != 5 {
		t.Fatal("应命中群维度对局")
	}

	s.Put(userKey, &Game{Length: 4, UpdatedAt: time.Now()})
	if g, ok := s.Find("qq", "g1", "u1"); !ok || g.Length != 4 {
		t.Fatal("存在用户维度对局时应优先返回")
	}

	// 用户对局结束后应回落到群对局
	if g, _ := s.Get(userKey); g != nil {
		g.Finished = true
	}
	if g, ok := s.Find("qq", "g1", "u1"); !ok || g.Length != 5 {
		t.Fatal("用户对局结束后应回落到群对局")
	}
}

func TestSessionStore_UpdateFound(t *testing.T) {
	s := NewSessionStore(time.Minute)
	key := SessionKey("qq", "g1", "", ScopeGroup)
	s.Put(key, &Game{Length: 5, MaxAttempts: 6, UpdatedAt: time.Now()})

	called := false
	ok := s.UpdateFound("qq", "g1", "u1", func(g *Game) {
		called = true
		g.Guesses = append(g.Guesses, Guess{Word: "crane"})
	})
	if !ok || !called {
		t.Fatal("UpdateFound 应命中并执行回调")
	}
	if g, _ := s.Get(key); len(g.Guesses) != 1 {
		t.Fatal("回调修改应写回对局")
	}

	if s.UpdateFound("qq", "g2", "u1", func(*Game) {}) {
		t.Fatal("不存在的会话不应命中")
	}
}

func TestSessionStore_Sweep(t *testing.T) {
	s := NewSessionStore(time.Minute)
	base := time.Now()
	s.now = func() time.Time { return base }
	s.Put("k", &Game{UpdatedAt: base})

	s.now = func() time.Time { return base.Add(30 * time.Second) }
	if got := s.Sweep(); len(got) != 0 {
		t.Fatalf("未超时不应清理，清理了 %d", len(got))
	}

	s.now = func() time.Time { return base.Add(2 * time.Minute) }
	abandoned := s.Sweep()
	if len(abandoned) != 1 {
		t.Fatalf("超时应清理 1 个，实际 %d", len(abandoned))
	}
	if s.Len() != 0 {
		t.Fatal("清理后会话表应为空")
	}
	// 未结束的对局应作为"弃局"返回，供调用方补记一次失败。
	if abandoned[0].Finished {
		t.Fatal("返回的弃局不应已标记为结束")
	}
}

// TestSessionStore_SweepSkipsFinished 验证已结束的对局只做内存回收、
// 不会被当作弃局再次结算。
func TestSessionStore_SweepSkipsFinished(t *testing.T) {
	s := NewSessionStore(time.Minute)
	base := time.Now()
	s.now = func() time.Time { return base }
	s.Put("f", &Game{UpdatedAt: base, Finished: true})

	s.now = func() time.Time { return base.Add(2 * time.Minute) }
	if got := s.Sweep(); len(got) != 0 {
		t.Fatalf("已结束的对局不应作为弃局返回，实际 %d", len(got))
	}
	if s.Len() != 0 {
		t.Fatal("已结束的对局也应被回收")
	}
}

func TestGame_RemainingAndHasGuessed(t *testing.T) {
	g := &Game{Length: 5, MaxAttempts: 2}
	if g.Remaining() != 2 {
		t.Fatalf("剩余应为 2，实际 %d", g.Remaining())
	}
	g.Guesses = append(g.Guesses, Guess{Word: "crane"})
	g.Used++
	if !g.HasGuessed("crane") || g.HasGuessed("slate") {
		t.Fatal("HasGuessed 判定错误")
	}
	if g.Remaining() != 1 {
		t.Fatalf("剩余应为 1，实际 %d", g.Remaining())
	}
	g.Finished = true
	if g.Remaining() != 0 {
		t.Fatalf("已结束剩余应为 0，实际 %d", g.Remaining())
	}
}

func TestGame_AddParticipant(t *testing.T) {
	g := &Game{}
	g.AddParticipant("", "空 ID 应被忽略")
	if len(g.Participants) != 0 {
		t.Fatal("空 userID 不应计入参与者")
	}

	g.AddParticipant("u1", "甲")
	g.AddParticipant("u2", "乙")
	g.AddParticipant("u1", "甲改名")
	if len(g.Participants) != 2 {
		t.Fatalf("重复参与应去重，实际 %d 人", len(g.Participants))
	}
	if g.Participants["u1"] != "甲改名" {
		t.Fatalf("重复参与应更新显示名，实际 %q", g.Participants["u1"])
	}
}

func TestGame_MultiBoardSolved(t *testing.T) {
	g := &Game{Length: 5, Answers: []string{"crane", "slate"}, Solved: []bool{false, false}}
	if g.BoardCount() != 2 || !g.Multi() {
		t.Fatalf("应识别为双谜底: boards=%d multi=%v", g.BoardCount(), g.Multi())
	}
	if g.AllSolved() || g.FirstUnsolved() != 0 {
		t.Fatal("初始状态不应判定为全部解出")
	}
	g.Solved[0] = true
	if g.AllSolved() || g.FirstUnsolved() != 1 {
		t.Fatal("仅解出一块时仍不应全部解出")
	}
	g.Solved[1] = true
	if !g.AllSolved() || g.FirstUnsolved() != -1 {
		t.Fatal("两块都解出后应判定为全部解出")
	}
	if got := g.AnswerList(); got != "CRANE / SLATE" {
		t.Fatalf("AnswerList = %q, 期望 CRANE / SLATE", got)
	}
}

func TestGame_FogVisible(t *testing.T) {
	g := &Game{Rules: RuleFog, FogRows: 3}
	if got := g.fogVisible(); got != 3 {
		t.Fatalf("fogVisible = %d, 期望 3", got)
	}
	g.FogRows = 0
	if got := g.fogVisible(); got != 1 {
		t.Fatalf("未指定行数时默认可见 1 行，实际 %d", got)
	}
	g.Rules = 0
	if got := g.fogVisible(); got != 0 {
		t.Fatalf("未启用迷雾时应为 0，实际 %d", got)
	}
}

func TestGame_Blind(t *testing.T) {
	if (&Game{}).blind() {
		t.Fatal("经典对局不应为盲猜")
	}
	if !(&Game{Rules: RuleBlind}).blind() {
		t.Fatal("--blind 规则应启用盲猜")
	}
	if !(&Game{Modifiers: ModBlind}).blind() {
		t.Fatal("混沌盲猜修饰符应启用盲猜")
	}
}

func TestDisplayUser(t *testing.T) {
	if got := displayUser("小明", "1234567890"); got != "小明" {
		t.Fatalf("应优先展示昵称，实际 %q", got)
	}
	if got := displayUser("   ", "1234567890"); got != shortID("1234567890") {
		t.Fatalf("昵称为空应回退短 ID，实际 %q", got)
	}
}

// TestGame_CanUseHint 验证免费提示上限与 --hint-cost 的交互：
// 默认受上限约束，启用 hint-cost 后不再受上限约束（改为每次消耗机会）。
func TestGame_CanUseHint(t *testing.T) {
	g := &Game{}
	if !g.CanUseHint(2) {
		t.Fatal("未使用提示时应可用")
	}
	g.HintsUsed = 2
	if g.CanUseHint(2) {
		t.Fatal("达到免费上限后应不可用")
	}
	if !g.CanUseHint(3) {
		t.Fatal("提高上限后应恢复可用")
	}
	g.Rules = RuleHintCost
	if !g.CanUseHint(0) {
		t.Fatal("启用 hint-cost 后不应再受免费上限约束")
	}
}
