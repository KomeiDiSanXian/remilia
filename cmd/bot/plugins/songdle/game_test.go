package songdle

import (
	"testing"
	"time"
)

func newTestGame(target Track, maxAttempts int) *Game {
	return &Game{
		Target:       target,
		MaxAttempts:  maxAttempts,
		Participants: make(map[string]string, 2),
	}
}

// mustProbe 构造一个必定成功的探测，便于在测试里简洁地驱动对局。
func mustProbe(t *testing.T, target Track, attr Attribute, raw string) Probe {
	t.Helper()
	pr, err := MakeProbe(target, attr, raw)
	if err != nil {
		t.Fatalf("MakeProbe(%v,%q): %v", attr, raw, err)
	}
	return pr
}

func TestSubmitTitleWin(t *testing.T) {
	target := Track{ID: "1", Title: "Future"}
	g := newTestGame(target, 5)
	if !g.Submit(mustProbe(t, target, AttrTitle, "future"), time.Now()) {
		t.Fatal("猜中标题应判胜")
	}
	if !g.Finished || !g.Won {
		t.Fatalf("应结束且获胜: finished=%v won=%v", g.Finished, g.Won)
	}
	if g.Attempts() != 1 || g.Remaining() != 0 {
		t.Errorf("次数统计错误: attempts=%d remaining=%d", g.Attempts(), g.Remaining())
	}
}

func TestSubmitProbeDoesNotWin(t *testing.T) {
	target := Track{ID: "1", Title: "Future", BPM: 130}
	g := newTestGame(target, 3)
	if g.Submit(mustProbe(t, target, AttrBPM, "130"), time.Now()) {
		t.Fatal("元数据命中不应判胜")
	}
	if g.Finished {
		t.Fatal("元数据探测后对局应继续")
	}
	if g.Attempts() != 1 || g.Remaining() != 2 {
		t.Errorf("次数统计错误: attempts=%d remaining=%d", g.Attempts(), g.Remaining())
	}
}

func TestSubmitLoseAfterMaxAttempts(t *testing.T) {
	target := Track{ID: "1", Title: "Future", BPM: 200}
	g := newTestGame(target, 2)
	if g.Submit(mustProbe(t, target, AttrBPM, "100"), time.Now()) {
		t.Fatal("未猜中标题不应判胜")
	}
	if g.Finished {
		t.Fatal("未用尽次数不应结束")
	}
	if g.Remaining() != 1 {
		t.Fatalf("剩余次数 = %d, 期望 1", g.Remaining())
	}
	g.Submit(mustProbe(t, target, AttrBPM, "300"), time.Now())
	if !g.Finished || g.Won {
		t.Fatalf("用尽次数应判负: finished=%v won=%v", g.Finished, g.Won)
	}
	if !g.OutOfAttempts() {
		t.Error("应报告次数已用尽")
	}
}

func TestHasProbed(t *testing.T) {
	target := Track{Title: "Future", BPM: 130}
	g := newTestGame(target, 5)
	pr := mustProbe(t, target, AttrBPM, "130")
	g.Submit(pr, time.Now())
	if !g.HasProbed(mustProbe(t, target, AttrBPM, "130")) {
		t.Error("同属性同取值应判为已探测")
	}
	if g.HasProbed(mustProbe(t, target, AttrBPM, "131")) {
		t.Error("不同取值不应判为已探测")
	}
	if g.HasProbed(mustProbe(t, target, AttrConst, "13")) {
		t.Error("不同属性不应判为已探测")
	}
}

func TestMatchedValueAndTried(t *testing.T) {
	target := Track{Title: "Future", Artist: "X", BPM: 130}
	g := newTestGame(target, 5)
	g.Submit(mustProbe(t, target, AttrArtist, "Y"), time.Now())
	if _, ok := g.MatchedValue(AttrArtist); ok {
		t.Error("未命中时不应报告已确认取值")
	}
	g.Submit(mustProbe(t, target, AttrArtist, "x"), time.Now())
	if v, ok := g.MatchedValue(AttrArtist); !ok || v != "x" {
		t.Errorf("命中后应返回确认取值，实际 %q,%v", v, ok)
	}
	if tried := g.TriedValues(AttrArtist); len(tried) != 2 {
		t.Errorf("TriedValues 应记录两次，实际 %v", tried)
	}

	reveal := newTestGame(Track{Artist: "Z"}, 5)
	reveal.RevealArtist = true
	if v, ok := reveal.MatchedValue(AttrArtist); !ok || v != "Z" {
		t.Errorf("--artist 应直接公布曲师，实际 %q,%v", v, ok)
	}
}

func TestNumericRange(t *testing.T) {
	target := Track{Title: "Future", BPM: 160}
	g := newTestGame(target, 6)
	g.Submit(mustProbe(t, target, AttrBPM, "150"), time.Now()) // 谜底更高 → 下界 150
	g.Submit(mustProbe(t, target, AttrBPM, "200"), time.Now()) // 谜底更低 → 上界 200
	r := g.NumericRange(AttrBPM)
	if !r.HasLo || r.Lo != 150 || !r.HasHi || r.Hi != 200 || r.Exact {
		t.Fatalf("区间推导错误: %+v", r)
	}
	g.Submit(mustProbe(t, target, AttrBPM, "160"), time.Now()) // 精确命中
	r = g.NumericRange(AttrBPM)
	if !r.Exact || r.ExactVal != 160 {
		t.Fatalf("精确命中应返回 Exact，实际 %+v", r)
	}
	if r := g.NumericRange(AttrArtist); r.HasLo || r.HasHi || r.Exact {
		t.Error("非数值属性不应推导区间")
	}
}

func TestAddParticipant(t *testing.T) {
	g := newTestGame(Track{}, 5)
	g.AddParticipant("u1", "甲")
	g.AddParticipant("u1", "")
	g.AddParticipant("", "空")
	if len(g.Participants) != 1 || g.Participants["u1"] != "甲" {
		t.Fatalf("参与者记录错误: %+v", g.Participants)
	}
}

func TestParseScope(t *testing.T) {
	if s, ok := ParseScope("user"); !ok || s != ScopeUser {
		t.Errorf("ParseScope(user) = %v,%v", s, ok)
	}
	if s, ok := ParseScope("GROUP"); !ok || s != ScopeGroup {
		t.Errorf("ParseScope(GROUP) = %v,%v", s, ok)
	}
	if _, ok := ParseScope("nope"); ok {
		t.Error("非法维度不应解析成功")
	}
}

func TestModeString(t *testing.T) {
	if ModeDaily.String() != "daily" || ModeRandom.String() != "random" {
		t.Error("Mode.String 映射错误")
	}
	if ScopeUser.String() != "user" || ScopeGroup.String() != "group" {
		t.Error("Scope.String 映射错误")
	}
}

func TestSubmitGuessWin(t *testing.T) {
	target := compareTarget
	g := newTestGame(target, 5)
	tg := TrackGuess{Track: target, Cells: Compare(target, target)}
	if !g.SubmitGuess(tg, time.Now()) {
		t.Fatal("猜中谜底应判胜")
	}
	if !g.Finished || !g.Won {
		t.Fatalf("应结束且获胜: finished=%v won=%v", g.Finished, g.Won)
	}
	if g.Attempts() != 1 || g.Remaining() != 0 {
		t.Errorf("次数统计错误: attempts=%d remaining=%d", g.Attempts(), g.Remaining())
	}
	if !g.HasGuessed(target) {
		t.Error("已猜过的曲目应被记录")
	}
}

func TestSubmitGuessLose(t *testing.T) {
	target := compareTarget
	other := Track{ID: "9", Title: "Other", Type: "DX", Version: "maimai", BPM: 100}
	g := newTestGame(target, 2)
	tg := TrackGuess{Track: other, Cells: Compare(target, other)}
	if g.SubmitGuess(tg, time.Now()) {
		t.Fatal("猜错不应判胜")
	}
	if g.Finished || g.Remaining() != 1 {
		t.Fatalf("第一次猜错后应继续: finished=%v remaining=%d", g.Finished, g.Remaining())
	}
	g.SubmitGuess(tg, time.Now())
	if !g.Finished || g.Won {
		t.Fatalf("用尽次数应判负: finished=%v won=%v", g.Finished, g.Won)
	}
}

func TestAttemptsCountsProbesAndGuesses(t *testing.T) {
	target := compareTarget
	g := newTestGame(target, 5)
	g.Submit(mustProbe(t, target, AttrBPM, "100"), time.Now())
	other := Track{ID: "9", Title: "Other"}
	g.SubmitGuess(TrackGuess{Track: other, Cells: Compare(target, other)}, time.Now())
	if g.Attempts() != 2 {
		t.Fatalf("探测 + 猜曲目应共消耗 2 次，实际 %d", g.Attempts())
	}
	if g.Remaining() != 3 {
		t.Errorf("剩余次数 = %d, 期望 3", g.Remaining())
	}
}

func TestHasGuessed(t *testing.T) {
	target := compareTarget
	g := newTestGame(target, 5)
	guessed := Track{ID: "9", Title: "Other"}
	g.SubmitGuess(TrackGuess{Track: guessed}, time.Now())
	if !g.HasGuessed(guessed) {
		t.Error("同 ID 应判为已猜过")
	}
	if g.HasGuessed(Track{ID: "10", Title: "Other"}) {
		t.Error("不同 ID 不应判为已猜过")
	}
	if g.HasGuessed(Track{Title: "Other"}) {
		t.Error("缺少 ID 的曲目不应判为已猜过")
	}
}
