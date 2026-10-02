package songdle

import (
	"strings"
	"time"
)

// Scope 对局隔离维度。
type Scope uint8

const (
	// ScopeGroup 群内共享一局：所有群成员探测同一首谜底。
	ScopeGroup Scope = iota
	// ScopeUser 每人独立一局：同群内各玩家的谜底互不影响。
	ScopeUser
)

// String 返回配置 / 展示用的维度名。
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
	// ModeDaily 每日同一题（按日期哈希选曲）。
	ModeDaily
)

// String 返回展示用的模式名。
func (m Mode) String() string {
	if m == ModeDaily {
		return "daily"
	}
	return "random"
}

// Game 一局进行中或已结束的对局。
type Game struct {
	ID        string
	Platform  string
	ChatID    string
	OwnerID   string
	OwnerName string
	Scope     Scope
	Mode      Mode
	Daily     bool

	// Target 是本局谜底，只有猜中它的曲名才算获胜。
	Target Track
	// Probes 依次保存每次属性猜测及其结果。
	Probes []Probe
	// Guesses 依次保存每次「猜整首曲目」及其逐列对比结果。
	Guesses []TrackGuess
	// MaxAttempts 是本局可用的猜测次数（每次探测或猜曲目各消耗 1 次）。
	MaxAttempts int
	// RevealArtist 为真时开局即公布曲师（更简单的玩法）。
	RevealArtist bool
	// Filter 是本局出题时使用的曲库筛选条件。
	Filter Filter

	// Participants 本局参与者，userID -> 显示名（提交过合法猜测的人）。
	Participants map[string]string

	CreatedAt time.Time
	UpdatedAt time.Time
	Finished  bool
	Won       bool
	// recorded 标记本局是否已写入统计，避免重复结算。
	recorded bool
}

// Attempts 返回已进行的猜测次数（属性探测 + 猜曲目）。
func (g *Game) Attempts() int { return len(g.Probes) + len(g.Guesses) }

// Remaining 返回剩余猜测次数；已结束的对局返回 0。
func (g *Game) Remaining() int {
	if g.Finished {
		return 0
	}
	if n := g.MaxAttempts - g.Attempts(); n > 0 {
		return n
	}
	return 0
}

// OutOfAttempts 报告是否已用尽猜测次数。
func (g *Game) OutOfAttempts() bool { return g.Attempts() >= g.MaxAttempts }

// HasProbed 报告同属性同取值是否已经猜过（避免重复浪费次数）。
func (g *Game) HasProbed(p Probe) bool {
	for _, x := range g.Probes {
		if x.Attr == p.Attr && normalizeTitle(x.Value) == normalizeTitle(p.Value) {
			return true
		}
	}
	return false
}

// HasGuessed 报告某首曲目是否已经猜过（避免重复浪费次数）。
func (g *Game) HasGuessed(t Track) bool {
	for _, x := range g.Guesses {
		if t.ID != "" && x.Track.ID == t.ID {
			return true
		}
	}
	return false
}

// AddParticipant 记录一名参与者（重复调用只更新显示名）。
func (g *Game) AddParticipant(userID, name string) {
	if userID == "" {
		return
	}
	if g.Participants == nil {
		g.Participants = make(map[string]string, 4)
	}
	if name != "" || g.Participants[userID] == "" {
		g.Participants[userID] = name
	}
}

// Submit 记录一次猜测并返回是否猜中。
//
// 只有曲名（[AttrTitle]）完全命中才算获胜；其余属性命中只是提供线索。次数用尽即判负。
func (g *Game) Submit(p Probe, now time.Time) bool {
	g.Probes = append(g.Probes, p)
	g.UpdatedAt = now
	if p.Attr == AttrTitle && p.Mark == Match {
		g.Finished, g.Won = true, true
		return true
	}
	if g.OutOfAttempts() {
		g.Finished, g.Won = true, false
	}
	return false
}

// SubmitGuess 记录一次「猜整首曲目」并返回是否猜中。次数用尽即判负。
func (g *Game) SubmitGuess(tg TrackGuess, now time.Time) bool {
	g.Guesses = append(g.Guesses, tg)
	g.UpdatedAt = now
	if tg.Solved() {
		g.Finished, g.Won = true, true
		return true
	}
	if g.OutOfAttempts() {
		g.Finished, g.Won = true, false
	}
	return false
}

// MatchedValue 返回某属性已确认命中的取值；尚未确认时 ok 为 false。
func (g *Game) MatchedValue(attr Attribute) (string, bool) {
	if attr == AttrArtist && g.RevealArtist && g.Target.Artist != "" {
		return g.Target.Artist, true
	}
	for _, p := range g.Probes {
		if p.Attr == attr && p.Mark == Match {
			return p.Value, true
		}
	}
	return "", false
}

// TriedValues 返回某属性已经猜过的全部取值（按先后顺序）。
func (g *Game) TriedValues(attr Attribute) []string {
	var out []string
	for _, p := range g.Probes {
		if p.Attr == attr {
			out = append(out, p.Value)
		}
	}
	return out
}

// NumRange 是从数值型探测结果推导出的取值范围（开区间）。
type NumRange struct {
	Lo, Hi       float64
	HasLo, HasHi bool
	Exact        bool
	ExactVal     float64
}

// NumericRange 汇总某数值属性所有探测结果，得到当前可推导的区间 / 精确值。
func (g *Game) NumericRange(attr Attribute) NumRange {
	var r NumRange
	if !attr.Numeric() {
		return r
	}
	for _, p := range g.Probes {
		if p.Attr != attr {
			continue
		}
		n, ok := ProbeNumber(attr, p.Value)
		if !ok {
			continue
		}
		if p.Mark == Match {
			r.Exact, r.ExactVal = true, n
			return r
		}
		switch p.Dir {
		case DirUp: // 谜底比 n 更高 → n 成为下界
			if !r.HasLo || n > r.Lo {
				r.Lo, r.HasLo = n, true
			}
		case DirDown: // 谜底比 n 更低 → n 成为上界
			if !r.HasHi || n < r.Hi {
				r.Hi, r.HasHi = n, true
			}
		}
	}
	return r
}

// AnswerLine 返回谜底的公布文本。
func (g *Game) AnswerLine() string { return g.Target.Display() }
