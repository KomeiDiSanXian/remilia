package dealornodeal

import (
	"errors"
	"math"
	"slices"
	"testing"
)

// withDeterministicRNG 固定随机源：逆序洗牌（可预测且非平凡）+ randIntN 恒 0。
func withDeterministicRNG(t *testing.T) {
	t.Helper()
	oldShuffle, oldIntN := randShuffle, randIntN
	randShuffle = func(n int, swap func(i, j int)) {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			swap(i, j)
		}
	}
	randIntN = func(int) int { return 0 }
	t.Cleanup(func() {
		randShuffle, randIntN = oldShuffle, oldIntN
	})
}

// openWholeRound 反复打开本轮剩余箱子，直到对局离开开箱阶段。
func openWholeRound(g *Game) error {
	for g.Phase == PhaseOpening {
		done := false
		for i := range g.Opened {
			if i != g.OwnCase && !g.Opened[i] {
				if err := g.Open(i + 1); err != nil {
					return err
				}
				done = true
				break
			}
		}
		if !done {
			return ErrNoCase
		}
	}
	return nil
}

// reachFinal 把一局推到最终抉择阶段（仅剩两个箱子）。
func reachFinal(t *testing.T) *Game {
	t.Helper()
	g := NewGame(Options{})
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	for g.Phase != PhaseFinal {
		if err := openWholeRound(g); err != nil {
			t.Fatalf("openWholeRound: %v", err)
		}
		if g.Phase != PhaseOffer {
			t.Fatalf("开满一轮后应进入报价阶段，实际 %v", g.Phase)
		}
		if err := g.NoDeal(); err != nil {
			t.Fatalf("NoDeal: %v", err)
		}
	}
	return g
}

func sumValues(values []int64) int64 {
	var s int64
	for _, v := range values {
		s += v
	}
	return s
}

func TestNewGame_AssignsAllValues(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{OwnerID: "u1", OwnerName: "A"})
	if g.Cases() != CaseCount || len(g.Opened) != CaseCount {
		t.Fatalf("箱数 = %d/%d, 期望 %d", g.Cases(), len(g.Opened), CaseCount)
	}
	if g.OwnCase != -1 || g.Phase != PhasePick {
		t.Fatalf("初始状态应为未选箱/选箱阶段，实际 own=%d phase=%v", g.OwnCase, g.Phase)
	}
	if len(g.Remaining()) != CaseCount {
		t.Fatalf("初始剩余箱数 = %d, 期望 %d", len(g.Remaining()), CaseCount)
	}
	got := append([]int64(nil), g.Values...)
	want := append([]int64(nil), DefaultValues...)
	slices.Sort(got)
	slices.Sort(want)
	if len(got) != len(want) {
		t.Fatalf("奖金数量 = %d, 期望 %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("奖金表被改动: %v", got)
		}
	}
}

func TestPickThenOpenRoundReachesOffer(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if g.Phase != PhaseOpening || g.OwnCase != 0 {
		t.Fatalf("Pick 后状态错误: phase=%v own=%d", g.Phase, g.OwnCase)
	}
	if g.ToOpenThisRound != g.Schedule[0] {
		t.Fatalf("首轮需开 %d, 期望 %d", g.ToOpenThisRound, g.Schedule[0])
	}
	if err := openWholeRound(g); err != nil {
		t.Fatalf("openWholeRound: %v", err)
	}
	if g.Phase != PhaseOffer || !g.OfferReady {
		t.Fatalf("开满一轮后应为报价阶段: phase=%v ready=%v", g.Phase, g.OfferReady)
	}
	if g.Offer != g.BankerOffer() {
		t.Fatalf("报价 = %d, 期望 %d", g.Offer, g.BankerOffer())
	}
	if g.RemainingToOpen() != 0 {
		t.Fatalf("报价阶段本轮待开箱数应为 0, 实际 %d", g.RemainingToOpen())
	}
}

func TestOpen_Errors(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.Open(1); !errors.Is(err, ErrPhase) {
		t.Fatalf("未选箱前开箱应报 ErrPhase, 实际 %v", err)
	}
	if err := g.Pick(99); !errors.Is(err, ErrBadCase) {
		t.Fatalf("越界箱号应报 ErrBadCase, 实际 %v", err)
	}
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := g.Open(1); !errors.Is(err, ErrOwnCase) {
		t.Fatalf("打开自己的箱应报 ErrOwnCase, 实际 %v", err)
	}
	if err := g.Open(2); err != nil {
		t.Fatalf("Open(2): %v", err)
	}
	if err := g.Open(2); !errors.Is(err, ErrCaseOpened) {
		t.Fatalf("重复开箱应报 ErrCaseOpened, 实际 %v", err)
	}
	if err := g.Open(0); !errors.Is(err, ErrBadCase) {
		t.Fatalf("箱号 0 应报 ErrBadCase, 实际 %v", err)
	}
}

func TestCheckOpen(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.CheckOpen(1); !errors.Is(err, ErrPhase) {
		t.Fatalf("未选箱前应报 ErrPhase, 实际 %v", err)
	}
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := g.CheckOpen(1); !errors.Is(err, ErrOwnCase) {
		t.Fatalf("自己的箱子应报 ErrOwnCase, 实际 %v", err)
	}
	if err := g.CheckOpen(0); !errors.Is(err, ErrBadCase) {
		t.Fatalf("箱号 0 应报 ErrBadCase, 实际 %v", err)
	}
	if err := g.CheckOpen(99); !errors.Is(err, ErrBadCase) {
		t.Fatalf("越界箱号应报 ErrBadCase, 实际 %v", err)
	}
	if err := g.CheckOpen(2); err != nil {
		t.Fatalf("可打开的箱子不应报错, 实际 %v", err)
	}
	if err := g.Open(2); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := g.CheckOpen(2); !errors.Is(err, ErrCaseOpened) {
		t.Fatalf("已打开的箱子应报 ErrCaseOpened, 实际 %v", err)
	}
}

func TestCheckOpen_RoundFilled(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	for i := 2; i <= 1+g.ToOpenThisRound; i++ {
		if err := g.Open(i); err != nil {
			t.Fatalf("Open(%d): %v", i, err)
		}
	}
	if g.Phase != PhaseOffer {
		t.Fatalf("本轮开满后应进入报价阶段, 实际 %v", g.Phase)
	}
	if err := g.CheckOpen(20); !errors.Is(err, ErrPhase) {
		t.Fatalf("本轮开满后继续开箱应报 ErrPhase, 实际 %v", err)
	}
}

func TestDeal(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := openWholeRound(g); err != nil {
		t.Fatalf("openWholeRound: %v", err)
	}
	offer := g.Offer
	if err := g.Deal(); err != nil {
		t.Fatalf("Deal: %v", err)
	}
	if !g.Finished || !g.Dealt || g.Won != offer {
		t.Fatalf("成交后状态错误: finished=%v dealt=%v won=%d offer=%d", g.Finished, g.Dealt, g.Won, offer)
	}
	if err := g.Deal(); !errors.Is(err, ErrPhase) {
		t.Fatalf("重复成交应报 ErrPhase, 实际 %v", err)
	}
}

func TestPlayToFinalThenKeep(t *testing.T) {
	withDeterministicRNG(t)
	g := reachFinal(t)
	rem := g.Remaining()
	if len(rem) != 2 {
		t.Fatalf("最终阶段应剩 2 箱, 实际 %d", len(rem))
	}
	if !g.ValidCase(g.OwnCaseNumber()) {
		t.Fatal("自己的箱子应在剩余集合内")
	}
	ownVal := g.Values[g.OwnCase]
	if err := g.Keep(); err != nil {
		t.Fatalf("Keep: %v", err)
	}
	if !g.Finished || g.Swapped {
		t.Fatalf("保留后状态错误: finished=%v swapped=%v", g.Finished, g.Swapped)
	}
	if g.Won != ownVal {
		t.Fatalf("保留奖金 = %d, 期望 %d", g.Won, ownVal)
	}
}

func TestFinalSwap(t *testing.T) {
	withDeterministicRNG(t)
	g := reachFinal(t)
	own := g.OwnCase
	other := -1
	for _, idx := range g.Remaining() {
		if idx != own {
			other = idx
		}
	}
	if other < 0 {
		t.Fatal("未找到可交换的箱子")
	}
	want := g.Values[other]
	if err := g.Swap(); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if !g.Swapped || g.OwnCase != other {
		t.Fatalf("交换后状态错误: swapped=%v own=%d", g.Swapped, g.OwnCase)
	}
	if g.Won != want {
		t.Fatalf("交换后奖金 = %d, 期望 %d", g.Won, want)
	}
}

func TestQuit(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	g.Quit()
	if !g.Finished || g.Won != 0 || g.Dealt {
		t.Fatalf("放弃后状态错误: %+v", g)
	}
}

func TestPickRandom(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.PickRandom(); err != nil {
		t.Fatalf("PickRandom: %v", err)
	}
	if g.OwnCaseNumber() != 1 {
		t.Fatalf("randIntN 恒 0 时应选 1 号箱, 实际 %d", g.OwnCaseNumber())
	}
}

func TestExpectedValueAndRemaining(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	want := float64(sumValues(DefaultValues)) / float64(len(DefaultValues))
	if math.Abs(g.ExpectedValue()-want) > 1e-9 {
		t.Fatalf("全量期望值 = %v, 期望 %v", g.ExpectedValue(), want)
	}
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	eliminated := g.Values[1]
	if err := g.Open(2); err != nil {
		t.Fatalf("Open: %v", err)
	}
	wantRemain := float64(sumValues(DefaultValues)-eliminated) / float64(CaseCount-1)
	if math.Abs(g.ExpectedValue()-wantRemain) > 1e-9 {
		t.Fatalf("开箱后期望值 = %v, 期望 %v", g.ExpectedValue(), wantRemain)
	}
	if got := g.EliminatedValues(); len(got) != 1 || got[0] != eliminated {
		t.Fatalf("已排除金额 = %v, 期望 [%d]", got, eliminated)
	}
}

func TestNiceRound(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{0, 0},
		{9, 9},
		{150, 150},
		{1234, 1200},
		{98765, 99000},
		{123456, 120000},
	}
	for _, c := range cases {
		if got := niceRound(c.in); got != c.want {
			t.Errorf("niceRound(%v) = %d, 期望 %d", c.in, got, c.want)
		}
	}
}

func TestOfferFactorGrowsWithRound(t *testing.T) {
	rounds := len(NewGame(Options{}).Schedule)
	if rounds < 2 {
		t.Fatalf("轮数 = %d, 至少应为 2", rounds)
	}
	prev := -1.0
	for round := range rounds {
		f := offerFactor(round, rounds)
		if f < prev {
			t.Fatalf("第 %d 轮系数 %v 低于前一轮 %v", round+1, f, prev)
		}
		prev = f
	}
	if first := offerFactor(0, rounds); math.Abs(first-0.20) > 1e-9 {
		t.Fatalf("首轮系数 = %v, 期望 0.20", first)
	}
	if last := offerFactor(rounds-1, rounds); math.Abs(last-0.92) > 1e-9 {
		t.Fatalf("末轮系数 = %v, 期望 0.92", last)
	}
}

func TestClampCases(t *testing.T) {
	cases := map[int]int{
		0: CaseCount, 1: MinCases, MinCases: MinCases,
		CaseCount: CaseCount, MaxCases: MaxCases, MaxCases + 5: MaxCases,
	}
	for in, want := range cases {
		if got := ClampCases(in); got != want {
			t.Errorf("ClampCases(%d) = %d, 期望 %d", in, got, want)
		}
	}
}

func TestValuesFor(t *testing.T) {
	// 26 箱精确等于经典奖金表。
	classic := ValuesFor(CaseCount)
	if len(classic) != CaseCount {
		t.Fatalf("26 箱数量 = %d", len(classic))
	}
	for i := range DefaultValues {
		if classic[i] != DefaultValues[i] {
			t.Fatalf("26 箱第 %d 项 = %d, 期望 %d", i, classic[i], DefaultValues[i])
		}
	}
	// 其他箱数：升序、互不相同、保留最低与最高奖金。
	for _, n := range []int{MinCases, 8, 16, 20, 25, 27, 30} {
		vals := ValuesFor(n)
		if len(vals) != n {
			t.Fatalf("%d 箱数量 = %d", n, len(vals))
		}
		for i := 1; i < len(vals); i++ {
			if vals[i] <= vals[i-1] {
				t.Fatalf("%d 箱未升序/有重复: %v", n, vals)
			}
		}
		if vals[0] != 1 || vals[len(vals)-1] != 1000000 {
			t.Fatalf("%d 箱首尾 = %d/%d, 期望 1/1000000", n, vals[0], vals[len(vals)-1])
		}
	}
}

func TestScheduleFor(t *testing.T) {
	classic := scheduleFor(CaseCount)
	want := []int{6, 5, 4, 3, 2, 1, 1, 1, 1}
	if len(classic) != len(want) {
		t.Fatalf("26 箱轮次表 = %v, 期望 %v", classic, want)
	}
	for i := range want {
		if classic[i] != want[i] {
			t.Fatalf("26 箱轮次表 = %v, 期望 %v", classic, want)
		}
	}
	for _, n := range []int{MinCases, 8, 10, 16, 20, 26, 30} {
		sched := scheduleFor(n)
		sum := 0
		for _, k := range sched {
			if k < 1 {
				t.Fatalf("%d 箱出现非正轮次: %v", n, sched)
			}
			sum += k
		}
		if sum != n-2 {
			t.Fatalf("%d 箱轮次表合计 = %d, 期望 %d (%v)", n, sum, n-2, sched)
		}
	}
}

func TestVoteResolution(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{Scope: ScopeGroup})
	g.AddParticipant("a", "A")
	g.AddParticipant("b", "B")
	g.AddParticipant("c", "C")
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := openWholeRound(g); err != nil {
		t.Fatalf("openWholeRound: %v", err)
	}
	if resolved, _ := g.Vote("a", true); resolved {
		t.Fatal("1/3 票不应结算")
	}
	resolved, dealWon := g.Vote("b", true)
	if !resolved || !dealWon {
		t.Fatalf("2/3 票应结算且成交, resolved=%v deal=%v", resolved, dealWon)
	}
	if err := g.ContinueAfterVote(dealWon); err != nil {
		t.Fatalf("ContinueAfterVote: %v", err)
	}
	if !g.Finished || !g.Dealt {
		t.Fatalf("成交票结算后应结束: finished=%v dealt=%v", g.Finished, g.Dealt)
	}
}

func TestVoteNodealMajorityAdvances(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{Scope: ScopeGroup})
	g.AddParticipant("a", "A")
	g.AddParticipant("b", "B")
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := openWholeRound(g); err != nil {
		t.Fatalf("openWholeRound: %v", err)
	}
	if resolved, _ := g.Vote("a", false); resolved {
		t.Fatal("1/2 票不应结算")
	}
	resolved, dealWon := g.Vote("b", false)
	if !resolved || dealWon {
		t.Fatalf("2/2 不成交票应结算, resolved=%v deal=%v", resolved, dealWon)
	}
	if err := g.ContinueAfterVote(false); err != nil {
		t.Fatalf("ContinueAfterVote: %v", err)
	}
	if g.Phase != PhaseOpening || g.Round != 1 {
		t.Fatalf("不成交后应进入第 2 轮开箱, 实际 phase=%v round=%d", g.Phase, g.Round)
	}
}

func TestResolveForceSettle(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{Scope: ScopeGroup})
	g.AddParticipant("a", "A")
	g.AddParticipant("b", "B")
	g.AddParticipant("c", "C")
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := openWholeRound(g); err != nil {
		t.Fatalf("openWholeRound: %v", err)
	}
	if resolved, _ := g.Vote("a", true); resolved {
		t.Fatal("1/3 票不应自动结算")
	}
	if !g.Resolve() {
		t.Fatal("强制结算应判定成交票领先")
	}
}

func TestCanAct(t *testing.T) {
	p := &Plugin{}
	solo := &Game{Scope: ScopeUser, OwnerID: "owner"}
	if !p.canAct("owner", solo) {
		t.Error("发起者应可操作单人局")
	}
	if p.canAct("other", solo) {
		t.Error("非发起者不应操作单人局")
	}
	group := &Game{Scope: ScopeGroup, OwnerID: "owner"}
	if !p.canAct("anyone", group) {
		t.Error("群投票局任何人可操作")
	}
	if !canEndGame("owner", group) {
		t.Error("发起者应可终止/结算")
	}
	if canEndGame("anyone", group) {
		t.Error("仅发起者可终止/结算")
	}
}
