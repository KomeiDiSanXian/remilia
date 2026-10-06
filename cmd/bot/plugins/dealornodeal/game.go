// Package dealornodeal 实现名为「成交不成交（Deal or No Deal）」的箱子里猜
// 奖金小游戏。
//
// 玩法：26 个箱子各藏一笔虚拟奖金，玩家先选定一个箱子作为自己的，然后按轮次
// 依次打开其余箱子揭示奖金；每轮结束后「银行家」按剩余箱子的期望值给出报价，
// 玩家可以「成交」拿走报价，或「不成交」继续开箱；最后剩两个箱子时可以选择
// 交换，然后揭晓自己箱子的真实奖金。
//
// 支持单人局（每人独立）与群投票局（群内任何人可开箱、对报价投票决定成交）。
// 奖金全程为局内虚拟数值，不落库、不计排行榜。
package dealornodeal

import (
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

const (
	// CaseCount 是经典箱数，也是未配置时的默认值。
	CaseCount = 26
	// MinCases / MaxCases 是允许的箱数范围。
	MinCases = 6
	MaxCases = 30
)

// DefaultValues 是 26 个箱子的奖金表（单位：虚拟美元）。
var DefaultValues = []int64{
	1, 5, 10, 25, 50, 75, 100, 200, 300, 400, 500, 750,
	1000, 2500, 5000, 7500, 10000, 25000, 50000, 75000,
	100000, 200000, 300000, 400000, 500000, 1000000,
}

// Ladder30 是扩展奖金阶梯：包含经典 26 档，另加 2 / 3 / 150 / 750000 四档，
// 供非 26 箱的自定义局按需等距取样。
var Ladder30 = []int64{
	1, 2, 3, 5, 10, 25, 50, 75, 100, 150,
	200, 300, 400, 500, 750, 1000, 2500, 5000, 7500, 10000,
	25000, 50000, 75000, 100000, 200000, 300000, 400000, 500000, 750000, 1000000,
}

// ClampCases 把箱数钳制到 [MinCases, MaxCases]；<=0 时返回经典 26。
func ClampCases(n int) int {
	switch {
	case n <= 0:
		return CaseCount
	case n < MinCases:
		return MinCases
	case n > MaxCases:
		return MaxCases
	default:
		return n
	}
}

// ValuesFor 返回 n 箱玩法使用的奖金表（升序、互不相同）。
//
// n == 26 时精确使用经典奖金表；否则从经典表（n < 26）或扩展阶梯（n > 26）
// 等距取样，保证最低与最高奖金始终保留。
func ValuesFor(n int) []int64 {
	n = ClampCases(n)
	switch {
	case n == CaseCount:
		return append([]int64(nil), DefaultValues...)
	case n < CaseCount:
		return pickValues(DefaultValues, n)
	default:
		return pickValues(Ladder30, n)
	}
}

// pickValues 从升序阶梯中等距取出 n 个值（含首尾）。
func pickValues(ladder []int64, n int) []int64 {
	if n >= len(ladder) {
		return append([]int64(nil), ladder...)
	}
	if n <= 1 {
		return []int64{ladder[len(ladder)-1]}
	}
	out := make([]int64, n)
	last := len(ladder) - 1
	for i := range out {
		idx := int(math.Round(float64(i) * float64(last) / float64(n-1)))
		out[i] = ladder[idx]
	}
	return out
}

// scheduleFor 生成每轮开箱数：合计恰为 n-2（最终剩 2 箱）。
//
// 先从 n/4 起逐轮减到 1，再补足差额；n == 26 时结果与经典
// 6/5/4/3/2/1/1/1/1 完全一致。
func scheduleFor(n int) []int {
	target := n - 2
	if target <= 0 {
		return nil
	}
	start := max(3, n/4)
	var sched []int
	sum := 0
	for k := start; k >= 1 && sum < target; k-- {
		sched = append(sched, k)
		sum += k
	}
	for sum < target {
		sched = append(sched, 1)
		sum++
	}
	// 兜底：起始轮过大导致超额时从后往前削减，保证总量恰为 n-2。
	for sum > target {
		trimmed := false
		for i := len(sched) - 1; i >= 0; i-- {
			if sched[i] > 1 {
				sched[i]--
				sum--
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	return sched
}

// 测试可替换的随机数钩子（与 wordle 的 timeNow 同风格）。
var (
	randIntN    = rand.IntN
	randShuffle = rand.Shuffle
)

// Phase 表示对局当前阶段。
type Phase uint8

const (
	// PhasePick 等待玩家选定自己的箱子。
	PhasePick Phase = iota
	// PhaseOpening 本轮开箱进行中。
	PhaseOpening
	// PhaseOffer 银行家已报价，等待成交/不成交。
	PhaseOffer
	// PhaseFinal 仅剩两个箱子，等待交换/保留抉择。
	PhaseFinal
	// PhaseFinished 对局结束。
	PhaseFinished
)

// String 返回阶段名（用于图片与配置）。
func (p Phase) String() string {
	switch p {
	case PhasePick:
		return "pick"
	case PhaseOpening:
		return "opening"
	case PhaseOffer:
		return "offer"
	case PhaseFinal:
		return "final"
	default:
		return "finished"
	}
}

// 玩法错误，命令层据此映射为本地化文案。
var (
	ErrBadCase    = errors.New("dealornodeal: 箱号非法")
	ErrCaseOpened = errors.New("dealornodeal: 该箱已打开")
	ErrOwnCase    = errors.New("dealornodeal: 不能打开自己的箱子")
	ErrPhase      = errors.New("dealornodeal: 当前阶段不允许该操作")
	ErrNoCase     = errors.New("dealornodeal: 没有可选箱子")
)

// Game 是一局进行中或已结束的对局。
//
// 所有可变字段都在 SessionStore 的锁内访问，命令层通过 UpdateFound 串行修改。
type Game struct {
	ID       string
	Platform string
	ChatID   string
	// OwnerID / OwnerName 是发起者（群投票局的成交结算与退出权限归其所有）。
	OwnerID   string
	OwnerName string
	Scope     Scope

	// Values 是每个箱子（下标 = 箱号-1）的奖金，开局洗牌后固定。
	Values []int64
	// Opened 标记每个箱子是否已打开。
	Opened []bool
	// Schedule 是每轮需开箱数（依箱数动态生成），长度即总轮数。
	Schedule []int
	// OwnCase 是玩家选定的箱子下标；未选定时为 -1。
	OwnCase int

	Phase Phase
	// Round 是当前轮次下标（0 起）。
	Round int
	// OpenedThisRound / ToOpenThisRound 是本轮已开/需开箱数。
	OpenedThisRound int
	ToOpenThisRound int

	// Offer 是当前银行家报价；OfferReady 表示当前报价尚未被处理。
	Offer      int64
	OfferReady bool
	// Dealt 表示玩家已接受报价。
	Dealt bool
	// Won 是最终带走的奖金（成交价或自己箱子的真实值）。
	Won int64
	// Swapped 记录最终抉择是否选择了交换。
	Swapped bool

	// Participants 是本局参与者，userID -> 显示名。
	Participants map[string]string
	// Votes 是群投票局中各成员对当前报价的投票：userID -> 是否成交。
	Votes map[string]bool

	CreatedAt time.Time
	UpdatedAt time.Time
	Finished  bool
}

// Options 是创建对局的参数。
type Options struct {
	Platform  string
	ChatID    string
	OwnerID   string
	OwnerName string
	Scope     Scope
	// Values 为空时使用 [DefaultValues]。
	Values []int64
	// Cases 在 Values 为空时决定奖金表规模；<=0 时使用经典 26。
	Cases int
	Now   time.Time
}

// NewGame 创建一局对局并洗牌分配奖金。
func NewGame(o Options) *Game {
	values := o.Values
	if len(values) == 0 {
		values = ValuesFor(o.Cases)
	}
	shuffled := make([]int64, len(values))
	copy(shuffled, values)
	randShuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	return &Game{
		ID:           newID(now),
		Platform:     o.Platform,
		ChatID:       o.ChatID,
		OwnerID:      o.OwnerID,
		OwnerName:    o.OwnerName,
		Scope:        o.Scope,
		Values:       shuffled,
		Opened:       make([]bool, len(shuffled)),
		Schedule:     scheduleFor(len(shuffled)),
		OwnCase:      -1,
		Phase:        PhasePick,
		Participants: map[string]string{},
		Votes:        map[string]bool{},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// Cases 返回箱数。
func (g *Game) Cases() int { return len(g.Values) }

// CaseNumber 返回下标对应的 1 起箱号。
func CaseNumber(idx int) int { return idx + 1 }

// ValidCase 报告 1 起箱号是否在范围内。
func (g *Game) ValidCase(n int) bool { return n >= 1 && n <= len(g.Values) }

// AddParticipant 记录参与者（同 ID 只保留最新显示名）。
func (g *Game) AddParticipant(userID, name string) {
	if userID == "" {
		return
	}
	if name == "" {
		name = userID
	}
	if g.Participants == nil {
		g.Participants = map[string]string{}
	}
	g.Participants[userID] = name
}

// ParticipantCount 返回参与者数量。
func (g *Game) ParticipantCount() int { return len(g.Participants) }

// Pick 选定自己的箱子并进入第一轮开箱。
func (g *Game) Pick(n int) error {
	if g.Phase != PhasePick {
		return ErrPhase
	}
	if !g.ValidCase(n) {
		return ErrBadCase
	}
	g.OwnCase = n - 1
	g.Phase = PhaseOpening
	g.Round = 0
	g.startRound()
	return nil
}

// PickRandom 随机选定一个箱子并进入第一轮开箱。
func (g *Game) PickRandom() error {
	if g.Phase != PhasePick {
		return ErrPhase
	}
	g.OwnCase = randIntN(g.Cases())
	g.Phase = PhaseOpening
	g.Round = 0
	g.startRound()
	return nil
}

// startRound 按当前轮次设置本轮需开箱数。
func (g *Game) startRound() {
	if len(g.Schedule) == 0 {
		g.ToOpenThisRound = 0
		g.OpenedThisRound = 0
		return
	}
	idx := min(g.Round, len(g.Schedule)-1)
	g.ToOpenThisRound = g.Schedule[idx]
	// 防御：极端情况下不超过剩余可开箱数。
	if remaining := len(g.Remaining()); remaining-1 < g.ToOpenThisRound {
		g.ToOpenThisRound = max(remaining-1, 1)
	}
	g.OpenedThisRound = 0
}

// Open 打开一个箱子；本轮开满后自动进入报价阶段。
func (g *Game) Open(n int) error {
	if err := g.CheckOpen(n); err != nil {
		return err
	}
	idx := n - 1
	g.Opened[idx] = true
	g.OpenedThisRound++
	if g.OpenedThisRound >= g.ToOpenThisRound {
		g.makeOffer()
	}
	return nil
}

// CheckOpen 返回箱号 n 在当前状态下能否打开：可打开返回 nil，否则返回原因
// （[ErrPhase] 阶段不符 / [ErrBadCase] 箱号非法 / [ErrCaseOpened] 已打开 /
// [ErrOwnCase] 是自己的箱子）。批量开箱据此逐箱报告被跳过的原因。
func (g *Game) CheckOpen(n int) error {
	if g.Phase != PhaseOpening {
		return ErrPhase
	}
	if !g.ValidCase(n) {
		return ErrBadCase
	}
	idx := n - 1
	if g.Opened[idx] {
		return ErrCaseOpened
	}
	if idx == g.OwnCase {
		return ErrOwnCase
	}
	return nil
}

// makeOffer 计算银行家报价并进入报价阶段。
func (g *Game) makeOffer() {
	g.Offer = g.BankerOffer()
	g.OfferReady = true
	g.Phase = PhaseOffer
	g.Votes = map[string]bool{}
}

// Remaining 返回尚未打开的箱子下标（含自己的箱子）。
func (g *Game) Remaining() []int {
	out := make([]int, 0, len(g.Values))
	for i := range g.Values {
		if !g.Opened[i] {
			out = append(out, i)
		}
	}
	return out
}

// RemainingToOpen 返回本轮还需打开几个箱子。
func (g *Game) RemainingToOpen() int {
	return max(g.ToOpenThisRound-g.OpenedThisRound, 0)
}

// RemainingValues 返回尚未打开箱子的奖金。
func (g *Game) RemainingValues() []int64 {
	rem := g.Remaining()
	out := make([]int64, len(rem))
	for i, idx := range rem {
		out[i] = g.Values[idx]
	}
	return out
}

// EliminatedValues 返回已打开箱子的奖金（即已排除的金额）。
func (g *Game) EliminatedValues() []int64 {
	var out []int64
	for i, opened := range g.Opened {
		if opened {
			out = append(out, g.Values[i])
		}
	}
	return out
}

// ExpectedValue 返回剩余箱子的平均奖金。
func (g *Game) ExpectedValue() float64 {
	vals := g.RemainingValues()
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += float64(v)
	}
	return sum / float64(len(vals))
}

// FinalOffer 报告当前报价是否为最后一轮（仅剩两个箱子）的报价。
func (g *Game) FinalOffer() bool {
	return len(g.Remaining()) <= 2
}

// BankerOffer 按当前轮次系数计算报价。
func (g *Game) BankerOffer() int64 {
	factor := offerFactor(g.Round, len(g.Schedule))
	if g.FinalOffer() {
		factor = 0.98
	}
	return niceRound(g.ExpectedValue() * factor)
}

// offerFactor 返回第 round 轮（共 rounds 轮）报价相对剩余期望值的系数：
// 从 0.20 线性升到 0.92，轮次越靠后越接近真实期望（最后报价另按 0.98）。
func offerFactor(round, rounds int) float64 {
	if rounds <= 1 {
		return 0.92
	}
	f := 0.20 + 0.72*float64(round)/float64(rounds-1)
	if f > 0.92 {
		f = 0.92
	}
	if f < 0.20 {
		f = 0.20
	}
	return f
}

// niceRound 把报价四舍五入到两位有效数字（大额按千位取整），更接近真实报价观感。
func niceRound(v float64) int64 {
	if v <= 0 {
		return 0
	}
	step := math.Pow(10, math.Floor(math.Log10(v))-1)
	if step < 1 {
		step = 1
	}
	return int64(math.Round(v/step) * step)
}

// Deal 接受当前报价，对局立即结束。
func (g *Game) Deal() error {
	if g.Phase != PhaseOffer || !g.OfferReady {
		return ErrPhase
	}
	g.Dealt = true
	g.Won = g.Offer
	g.Finished = true
	g.OfferReady = false
	g.Phase = PhaseFinished
	return nil
}

// NoDeal 拒绝当前报价：最后一轮报价后进入交换抉择，否则进入下一轮开箱。
func (g *Game) NoDeal() error {
	if g.Phase != PhaseOffer || !g.OfferReady {
		return ErrPhase
	}
	g.OfferReady = false
	g.Votes = map[string]bool{}
	if g.FinalOffer() {
		g.Phase = PhaseFinal
		return nil
	}
	g.Round++
	g.Phase = PhaseOpening
	g.startRound()
	return nil
}

// Swap 在最终抉择中与自己箱子外的另一个箱子交换。
func (g *Game) Swap() error {
	if g.Phase != PhaseFinal {
		return ErrPhase
	}
	rem := g.Remaining()
	if len(rem) != 2 {
		return ErrNoCase
	}
	for _, idx := range rem {
		if idx != g.OwnCase {
			g.OwnCase = idx
			break
		}
	}
	g.Swapped = true
	return g.Reveal()
}

// Keep 在最终抉择中保留自己的箱子并揭晓。
func (g *Game) Keep() error {
	if g.Phase != PhaseFinal {
		return ErrPhase
	}
	return g.Reveal()
}

// Reveal 揭晓自己箱子的真实奖金并结束对局。
func (g *Game) Reveal() error {
	if g.OwnCase < 0 || g.OwnCase >= len(g.Values) {
		return ErrNoCase
	}
	g.Won = g.Values[g.OwnCase]
	g.Finished = true
	g.Phase = PhaseFinished
	return nil
}

// Quit 放弃对局（奖金记为 0）。
func (g *Game) Quit() {
	g.Finished = true
	g.Won = 0
	g.Dealt = false
	g.OfferReady = false
	g.Phase = PhaseFinished
}

// Vote 在群投票局中投票（deal=true 表示同意成交），返回是否已产生结果。
//
// 结算规则：当任意一方票数超过参与者总数的半数时立即结算，票多者胜。
func (g *Game) Vote(userID string, deal bool) (resolved, dealWon bool) {
	if g.Phase != PhaseOffer || !g.OfferReady {
		return false, false
	}
	if g.Votes == nil {
		g.Votes = map[string]bool{}
	}
	g.Votes[userID] = deal
	return g.tally()
}

// Resolve 强制结算当前投票（平票按不成交处理），返回是否成交。
func (g *Game) Resolve() bool {
	if g.Phase != PhaseOffer || !g.OfferReady {
		return false
	}
	deal, nodeal := g.voteCounts()
	return deal > nodeal
}

func (g *Game) voteCounts() (deal, nodeal int) {
	for _, d := range g.Votes {
		if d {
			deal++
		} else {
			nodeal++
		}
	}
	return deal, nodeal
}

// VoteCounts 返回当前成交/不成交票数（供展示）。
func (g *Game) VoteCounts() (deal, nodeal int) { return g.voteCounts() }

// tally 判断当前票数是否已足以结算。
func (g *Game) tally() (resolved, dealWon bool) {
	deal, nodeal := g.voteCounts()
	total := g.ParticipantCount()
	if total <= 0 {
		total = 1
	}
	win := max(deal, nodeal)
	if win*2 <= total {
		return false, false
	}
	return true, deal > nodeal
}

// ContinueAfterVote 把票决结果应用到对局。
func (g *Game) ContinueAfterVote(dealWon bool) error {
	if dealWon {
		return g.Deal()
	}
	return g.NoDeal()
}

// OwnCaseNumber 返回自己箱子号；未选定时返回 0。
func (g *Game) OwnCaseNumber() int {
	if g.OwnCase < 0 {
		return 0
	}
	return g.OwnCase + 1
}

// OwnValue 返回自己箱子的真实奖金。
func (g *Game) OwnValue() int64 {
	if g.OwnCase < 0 || g.OwnCase >= len(g.Values) {
		return 0
	}
	return g.Values[g.OwnCase]
}

// newID 生成对局 ID（时间戳 + 随机后缀，仅用于日志与调试）。
func newID(now time.Time) string {
	return now.Format("20060102150405") + "-" + string([]byte{byte('a' + randIntN(26)), byte('a' + randIntN(26))})
}
