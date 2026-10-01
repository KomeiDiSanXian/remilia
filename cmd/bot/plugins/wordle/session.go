package wordle

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Scope 对局隔离维度。
type Scope uint8

const (
	// ScopeGroup 群内共享一局：群成员共同作答同一棋盘。
	ScopeGroup Scope = iota
	// ScopeUser 每人独立棋盘：同群内各玩家的对局互不影响。
	ScopeUser
)

// String 返回配置/展示用的维度名。
func (s Scope) String() string {
	if s == ScopeUser {
		return "user"
	}
	return "group"
}

// ParseScope 解析隔离维度字符串，无法识别时 ok 为 false。
func ParseScope(s string) (Scope, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "user", "private", "personal":
		return ScopeUser, true
	case "group", "chat", "shared":
		return ScopeGroup, true
	default:
		return ScopeGroup, false
	}
}

// Mode 出题模式。
type Mode uint8

const (
	// ModeRandom 随机出题。
	ModeRandom Mode = iota
	// ModeDaily 每日同一题（按日期哈希选词）。
	ModeDaily
)

// String 返回配置/展示用的模式名。
func (m Mode) String() string {
	if m == ModeDaily {
		return "daily"
	}
	return "random"
}

// Game 一局进行中或已结束的对局。
type Game struct {
	// Deadline 限时模式下的当前作答截止时间。
	Deadline time.Time
	// LinkStartedAt 当前这一题的开始时间（连锁模式换题后重置）。
	LinkStartedAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// Score 抢分模式的个人贡献分：userID -> 分数。
	Score map[string]int
	// Participants 本局参与者，userID -> 显示名。
	//
	// 只有"提交过合法猜测"的人才会计入（发起、提示、放弃都不算），结算时每人各
	// 记一次战绩。这样群维度协作局里发起者也必须真正参与才能拿到胜利，路人与
	// 只点提示的人不会白捡战绩。
	Participants map[string]string
	ID           string
	Platform     string
	ChatID       string
	OwnerID      string
	OwnerName    string
	// Answers 是本局全部谜底：单谜底时长度为 1，双谜底/多谜底时依次排列。
	Answers []string
	// Solved 标记每个谜底是否已被猜中（与 Answers 等长）。
	Solved   []bool
	Guesses  []Guess
	Revealed []int // 已通过 /wordle hint 揭示的字母位置
	// Excluded 已通过 /wordle hint exclude 确认不在谜底中的字母。
	Excluded    []rune
	Length      int
	MaxAttempts int
	// Unlimited 标记无限机会（--unlimited）：猜测不消耗机会、永不判负，
	// 默认也只作为练习局记录历史、不计入战绩与排行榜。
	Unlimited bool
	// BlitzWindow 限时模式下每次作答的时限。
	BlitzWindow time.Duration
	// Used 已消耗的作答次数（提示经济与 COSTx2 都会累加）。
	Used int
	// HintsUsed 本局已使用的提示次数（用于免费提示上限；连锁换题后重置）。
	HintsUsed int
	// ChainIndex 连锁模式当前进行到第几题（0 起）。
	ChainIndex int
	// ChainWins 连锁模式已连续解出的题数。
	ChainWins int
	// FogRows 迷雾模式下保留可见的最近行数。
	FogRows int
	// ColorFog 颜色预算：每行最多保留着色的格子数（0 = 未启用）。
	ColorFog int
	// DecayRows 颜色衰减：只保留最近若干行的颜色（0 = 未启用）。
	DecayRows int
	// UnknownCells 未知格：每行随机隐藏的格子数（0 = 未启用）。
	UnknownCells int
	// DecoyCells 诱饵：每行随机谎报为黄色的灰色格子数（0 = 未启用）。
	DecoyCells int
	// DelayedRows 延迟着色：最新的若干行暂不显示颜色（0 = 未启用）。
	DelayedRows int
	// Rules 本局启用的玩法修饰符（零值 = 经典规则）。
	Rules RuleSet
	Scope Scope
	Mode  Mode
	// Modifiers 混沌模式抽到的修饰符。
	Modifiers Modifier
	Finished  bool
	Won       bool
}

// AddParticipant 记录一名参与者（重复调用只更新显示名）。
func (g *Game) AddParticipant(userID, name string) {
	if userID == "" {
		return
	}
	if g.Participants == nil {
		g.Participants = make(map[string]string, 4)
	}
	g.Participants[userID] = name
}

// AttemptCost 返回一次猜测消耗的作答次数（COSTx2 修饰符下为 2）。
func (g *Game) AttemptCost() int {
	if g.Modifiers.Has(ModDouble) {
		return 2
	}
	return 1
}

// BoardCount 返回棋盘（谜底）数量，至少为 1。
func (g *Game) BoardCount() int {
	if len(g.Answers) == 0 {
		return 1
	}
	return len(g.Answers)
}

// AnswerAt 返回第 i 个谜底；越界时返回空串。
func (g *Game) AnswerAt(i int) string {
	if i < 0 || i >= len(g.Answers) {
		return ""
	}
	return g.Answers[i]
}

// Multi 报告是否为多谜底模式。
func (g *Game) Multi() bool { return len(g.Answers) > 1 }

// AllSolved 报告是否所有谜底都已被猜中。
func (g *Game) AllSolved() bool {
	if len(g.Answers) == 0 {
		return false
	}
	for i := range g.Answers {
		if i >= len(g.Solved) || !g.Solved[i] {
			return false
		}
	}
	return true
}

// FirstUnsolved 返回第一个尚未解出的谜底下标；全部解出时返回 -1。
func (g *Game) FirstUnsolved() int {
	for i := range g.Answers {
		if i >= len(g.Solved) || !g.Solved[i] {
			return i
		}
	}
	return -1
}

// AnswerList 返回大写谜底，多谜底用 " / " 连接（用于公布答案）。
func (g *Game) AnswerList() string {
	if len(g.Answers) == 0 {
		return ""
	}
	parts := make([]string, 0, len(g.Answers))
	for _, a := range g.Answers {
		parts = append(parts, strings.ToUpper(a))
	}
	return strings.Join(parts, " / ")
}

// blind 报告本局是否启用盲猜（规则或混沌修饰符皆可）。
func (g *Game) blind() bool {
	return g.Rules.Has(RuleBlind) || g.Modifiers.Has(ModBlind)
}

// fogVisible 返回迷雾模式下应保留的最近行数；未启用迷雾时返回 0。
func (g *Game) fogVisible() int {
	if !g.Rules.Has(RuleFog) {
		return 0
	}
	if g.FogRows <= 0 {
		return 1
	}
	return g.FogRows
}

// LinkStart 返回当前这一题的开始时间，未设置时回退到对局创建时间。
func (g *Game) LinkStart() time.Time {
	if g.LinkStartedAt.IsZero() {
		return g.CreatedAt
	}
	return g.LinkStartedAt
}

// OutOfAttempts 报告剩余次数是否已不足以再猜一次。
func (g *Game) OutOfAttempts() bool {
	if g.Unlimited {
		return false
	}
	return g.Remaining() < g.AttemptCost()
}

// Expired 报告限时对局是否已超过当前作答时限。
func (g *Game) Expired(now time.Time) bool {
	return g.Rules.Has(RuleBlitz) && !g.Deadline.IsZero() && now.After(g.Deadline)
}

// Remaining 返回剩余可猜次数。
func (g *Game) Remaining() int {
	// 无限机会没有"剩余次数"的概念，返回 0；展示层请改用 [Game.Unlimited]。
	if g.Finished || g.Unlimited {
		return 0
	}
	if n := g.MaxAttempts - g.Used; n > 0 {
		return n
	}
	return 0
}

// MaxGuesses 返回本局最多可以提交的猜测次数（按每次猜测的消耗折算）。
//
// COSTx2 修饰符下每次猜测消耗 2 次机会，因此"可猜次数"是次数上限的一半；
// 状态栏展示可猜次数比展示原始次数更直观，也避免"剩余 6/6 却只能猜 3 次"的误导。
func (g *Game) MaxGuesses() int {
	if g.Unlimited {
		return 0
	}
	cost := g.AttemptCost()
	if cost <= 0 {
		cost = 1
	}
	return g.MaxAttempts / cost
}

// RemainingGuesses 返回本局还可提交的猜测次数（按消耗折算）。
func (g *Game) RemainingGuesses() int {
	cost := g.AttemptCost()
	if cost <= 0 {
		cost = 1
	}
	return g.Remaining() / cost
}

// HintsAllowed 报告本局是否允许使用提示。
func (g *Game) HintsAllowed() bool { return !g.Modifiers.Has(ModNoHint) }

// CanUseHint 报告当前是否还能再用一次提示。
//
// 启用 --hint-cost（RuleHintCost）时每次提示都消耗机会，因此始终可用；
// 否则受 maxFree 次免费提示上限约束，避免逐格揭示直接白嫖胜利。
func (g *Game) CanUseHint(maxFree int) bool {
	if g.Rules.Has(RuleHintCost) {
		return true
	}
	return g.HintsUsed < maxFree
}

// AddScore 累加某位玩家在本局的贡献分。
func (g *Game) AddScore(userID string, delta int) {
	if userID == "" || delta == 0 {
		return
	}
	if g.Score == nil {
		g.Score = make(map[string]int, 4)
	}
	g.Score[userID] += delta
}

// ScoreEntry 是抢分榜上的一条记录。
type ScoreEntry struct {
	UserID string
	Name   string
	Score  int
}

// Scoreboard 返回按得分降序（同分按 userID 稳定）排列的计分榜。
func (g *Game) Scoreboard() []ScoreEntry {
	out := make([]ScoreEntry, 0, len(g.Score))
	for uid, score := range g.Score {
		if score <= 0 {
			continue
		}
		out = append(out, ScoreEntry{UserID: uid, Name: g.Participants[uid], Score: score})
	}
	slices.SortFunc(out, func(a, b ScoreEntry) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return strings.Compare(a.UserID, b.UserID)
	})
	return out
}

// advanceChain 结算完当前这一题后，重置状态进入连锁的下一题。
func (g *Game) advanceChain(answers []string) {
	g.ChainIndex++
	g.ChainWins++
	g.Answers = answers
	g.Solved = make([]bool, len(answers))
	g.Guesses = nil
	g.Revealed = nil
	g.Excluded = nil
	g.Used = 0
	g.HintsUsed = 0
	g.Participants = make(map[string]string, 4)
	g.Score = nil
	g.Won = false
	g.Finished = false
	g.LinkStartedAt = timeNow()
	g.UpdatedAt = g.LinkStartedAt
	if g.Rules.Has(RuleBlitz) {
		g.Deadline = g.LinkStartedAt.Add(g.BlitzWindow)
	}
	if g.Rules.Has(RuleGauntlet) {
		// 机会下限按单次猜测的消耗折算，避免 COSTx2 时"下限 2"被消耗 2 后只剩 1 次猜测。
		g.MaxAttempts = max(g.MaxAttempts-1, 2*g.AttemptCost())
	}
}

// HasGuessed 报告某个单词是否已经猜过。
func (g *Game) HasGuessed(word string) bool {
	for _, x := range g.Guesses {
		if x.Word == word {
			return true
		}
	}
	return false
}

// SolvedPositionsOf 返回第 board 块棋盘所有已被猜中（绿色）的位置。
func (g *Game) SolvedPositionsOf(board int) map[int]bool {
	out := make(map[int]bool, g.Length)
	for _, x := range g.Guesses {
		for i, m := range x.MarksFor(board) {
			if m == Correct {
				out[i] = true
			}
		}
	}
	return out
}

// SolvedPositions 返回第一块棋盘已猜中的位置（单谜底即全部）。
func (g *Game) SolvedPositions() map[int]bool { return g.SolvedPositionsOf(0) }

// SessionStore 内存会话表：按会话键隔离对局，并按空闲时间回收。
type SessionStore struct {
	mu    sync.Mutex
	games map[string]*Game
	ttl   time.Duration
	now   func() time.Time
}

// NewSessionStore 创建会话表；ttl <= 0 时使用默认 30 分钟。
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &SessionStore{games: make(map[string]*Game), ttl: ttl, now: time.Now}
}

// SessionKey 生成会话键：user 维度附加用户 ID，group 维度忽略用户。
func SessionKey(platform, chatID, userID string, scope Scope) string {
	if scope == ScopeUser {
		return platform + "|" + chatID + "|" + userID
	}
	return platform + "|" + chatID
}

// Get 按会话键取对局。
func (s *SessionStore) Get(key string) (*Game, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[key]
	return g, ok
}

// Find 按平台/会话/用户查找对局：优先用户维度，其次群维度；都优先未结束的对局。
func (s *SessionStore) Find(platform, chatID, userID string) (*Game, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userKey := SessionKey(platform, chatID, userID, ScopeUser)
	groupKey := SessionKey(platform, chatID, "", ScopeGroup)
	u, uok := s.games[userKey]
	g, gok := s.games[groupKey]
	if uok && !u.Finished {
		return u, true
	}
	if gok && !g.Finished {
		return g, true
	}
	if uok {
		return u, true
	}
	if gok {
		return g, true
	}
	return nil, false
}

// UpdateFound 在锁内查找并修改对局，保证同一对局的并发猜测串行执行。
//
// 查找顺序与 [SessionStore.Find] 一致；fn 在持锁状态下执行，不应阻塞。
// 未找到任何对局时返回 false。
func (s *SessionStore) UpdateFound(platform, chatID, userID string, fn func(*Game)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	userKey := SessionKey(platform, chatID, userID, ScopeUser)
	groupKey := SessionKey(platform, chatID, "", ScopeGroup)

	apply := func(key string, requireActive bool) bool {
		g, ok := s.games[key]
		if !ok || (requireActive && g.Finished) {
			return false
		}
		fn(g)
		return true
	}
	if apply(userKey, true) || apply(groupKey, true) {
		return true
	}
	// 兜底：只有已结束的对局，也允许 fn 读取用于提示。
	return apply(userKey, false) || apply(groupKey, false)
}

// Put 写入对局。
func (s *SessionStore) Put(key string, g *Game) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.games[key] = g
}

// Delete 删除对局。
func (s *SessionStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.games, key)
}

// Len 返回当前对局数。
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.games)
}

// Sweep 清理空闲超时的对局，并返回其中"尚未结束"的对局。
//
// 未结束就被回收意味着玩家弃局：调用方需要把它们结算为失败，否则只要挂机
// 就能逃掉一次败场、把胜率刷高。已结束的对局直接丢弃即可。
func (s *SessionStore) Sweep() []*Game {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-s.ttl)
	var abandoned []*Game
	for k, g := range s.games {
		if !g.UpdatedAt.Before(cutoff) {
			continue
		}
		delete(s.games, k)
		if !g.Finished {
			abandoned = append(abandoned, g)
		}
	}
	return abandoned
}

// MarkExpired 把已超时的限时对局标记为失败并返回，供调用方结算统计。
//
// 只做标记不做删除：仍需保留对局，等待下一次交互时向用户公布结果。
func (s *SessionStore) MarkExpired(now time.Time) []*Game {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Game
	for _, g := range s.games {
		if g.Finished || !g.Rules.Has(RuleBlitz) || g.Deadline.IsZero() {
			continue
		}
		if now.After(g.Deadline) {
			g.Finished = true
			g.Won = false
			g.UpdatedAt = now
			out = append(out, g)
		}
	}
	return out
}
