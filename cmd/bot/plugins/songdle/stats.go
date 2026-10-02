package songdle

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

// maxAttempts 是猜测次数分布统计的下标上界，超过时并入最后一档。
const maxAttempts = 30

// StatRecord 每用户的累计统计（GORM 模型）。
type StatRecord struct {
	ID uint `gorm:"primaryKey;autoIncrement"`

	UserID string `gorm:"column:user_id;uniqueIndex:idx_songdle_stat_user;not null;size:128"`
	// Name 是最近一次参与对局时使用的显示名，为空时展示方回退到短 ID。
	Name string `gorm:"size:64"`

	// Played 参与并结算的对局数；Won 其中成功猜中的局数。
	Played int `gorm:"not null"`
	Won    int `gorm:"not null"`
	// Streak / BestStreak 当前连胜与历史最高连胜。
	Streak     int `gorm:"not null"`
	BestStreak int `gorm:"not null"`
	// BestAttempts 最少用多少次猜中；0 表示还没有猜中过。
	BestAttempts int `gorm:"not null"`

	// GuessDist 猜中所需次数的分布 JSON，下标 0 表示失败局数，
	// 下标 1..maxAttempts 对应首次猜中的猜测次数。
	GuessDist string `gorm:"type:text"`
	// LastDailyDay 最后一次参与每日题的日期（YYYY-MM-DD）。
	LastDailyDay string `gorm:"size:16"`

	UpdatedAt time.Time
}

// TableName 指定统计表名。
func (StatRecord) TableName() string { return "songdle_stats" }

// DailyLock 记录某个维度当日是否已经开过每日题。
type DailyLock struct {
	Key       string `gorm:"column:lock_key;primaryKey;size:160"`
	Day       string `gorm:"size:16"`
	UpdatedAt time.Time
}

// TableName 指定每日锁表名。
func (DailyLock) TableName() string { return "songdle_daily_locks" }

// GameRecord 已完成对局的历史记录（GORM 模型）。
type GameRecord struct {
	ID uint `gorm:"primaryKey;autoIncrement"`

	Platform string `gorm:"index;size:32"`
	ChatID   string `gorm:"index;size:128"`
	UserID   string `gorm:"index;size:128"`
	Scope    string `gorm:"size:16"`
	Mode     string `gorm:"size:16"`

	// Answer 保存谜底（曲名 — 曲师）。
	Answer string `gorm:"size:256"`
	// Probes 保存探测 / 猜测序列 JSON（每项形如 "attr=value"）。
	Probes     string `gorm:"type:text"`
	Won        bool
	Attempts   int
	DurationMs int64
	CreatedAt  time.Time `gorm:"index"`
}

// TableName 指定对局表名。
func (GameRecord) TableName() string { return "songdle_games" }

// Stat 内存中的用户统计。
type Stat struct {
	UserID       string
	Name         string
	Played       int
	Won          int
	Streak       int
	BestStreak   int
	BestAttempts int
	GuessDist    [maxAttempts + 1]int // 下标 0 = 失败，1..maxAttempts = 首次猜中的次数
	LastDailyDay string
}

// WinRate 返回胜率（百分比）；无对局时为 0。
func (s *Stat) WinRate() float64 {
	if s.Played == 0 {
		return 0
	}
	return float64(s.Won) / float64(s.Played) * 100
}

// Record 记录一局结果。won 为 false 时中断连胜。
func (s *Stat) Record(won bool, attempts int) {
	s.Played++
	if !won {
		s.Streak = 0
		s.GuessDist[0]++
		return
	}
	s.Won++
	s.Streak++
	if s.Streak > s.BestStreak {
		s.BestStreak = s.Streak
	}
	if attempts > 0 && (s.BestAttempts == 0 || attempts < s.BestAttempts) {
		s.BestAttempts = attempts
	}
	idx := min(max(attempts, 1), maxAttempts)
	s.GuessDist[idx]++
}

// WinRate 返回胜率（百分比）；无对局时为 0。
func (r StatRecord) WinRate() float64 {
	if r.Played == 0 {
		return 0
	}
	return float64(r.Won) / float64(r.Played) * 100
}

// store 基于 storage 插件（GORM/SQLite）的持久化实现。
type store struct {
	db *storage.Plugin
}

func newStore(db *storage.Plugin) *store { return &store{db: db} }

func (s *store) migrate() error {
	return s.db.AutoMigrate(&StatRecord{}, &DailyLock{}, &GameRecord{})
}

// loadStat 读取用户统计；不存在时返回空统计而非错误。
func (s *store) loadStat(userID string) (*Stat, error) {
	var rec StatRecord
	err := s.db.First(&rec, "user_id = ?", userID)
	if errors.Is(err, storage.ErrNotFound) {
		return &Stat{UserID: userID}, nil
	}
	if err != nil {
		return nil, err
	}
	st := &Stat{
		UserID:       userID,
		Name:         rec.Name,
		Played:       rec.Played,
		Won:          rec.Won,
		Streak:       rec.Streak,
		BestStreak:   rec.BestStreak,
		BestAttempts: rec.BestAttempts,
		LastDailyDay: rec.LastDailyDay,
	}
	if rec.GuessDist != "" {
		_ = json.Unmarshal([]byte(rec.GuessDist), &st.GuessDist)
	}
	return st, nil
}

// saveStat 写入用户统计（不存在则新建，存在则更新）。
func (s *store) saveStat(st *Stat) error {
	dist, err := json.Marshal(st.GuessDist)
	if err != nil {
		return err
	}
	rec := StatRecord{
		UserID:       st.UserID,
		Name:         st.Name,
		Played:       st.Played,
		Won:          st.Won,
		Streak:       st.Streak,
		BestStreak:   st.BestStreak,
		BestAttempts: st.BestAttempts,
		GuessDist:    string(dist),
		LastDailyDay: st.LastDailyDay,
		UpdatedAt:    time.Now(),
	}

	var existing StatRecord
	err = s.db.First(&existing, "user_id = ?", st.UserID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return s.db.Create(&rec)
	case err != nil:
		return err
	default:
		rec.ID = existing.ID
		return s.db.Save(&rec)
	}
}

// dailyLockDay 返回该维度当日已锁定的日期，未锁定时返回空串。
func (s *store) dailyLockDay(key string) (string, error) {
	var rec DailyLock
	err := s.db.First(&rec, "lock_key = ?", key)
	if errors.Is(err, storage.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return rec.Day, nil
}

// setDailyLock 锁定该维度当日已开局。
func (s *store) setDailyLock(key, day string) error {
	return s.db.Save(&DailyLock{Key: key, Day: day, UpdatedAt: time.Now()})
}

// recordGame 写入一局历史记录。
func (s *store) recordGame(g *Game, userID string, durationMs int64) error {
	probes, err := json.Marshal(append(probeTrail(g.Probes), guessTrail(g.Guesses)...))
	if err != nil {
		return err
	}
	rec := GameRecord{
		Platform:   g.Platform,
		ChatID:     g.ChatID,
		UserID:     userID,
		Scope:      g.Scope.String(),
		Mode:       g.Mode.String(),
		Answer:     g.AnswerLine(),
		Probes:     string(probes),
		Won:        g.Won,
		Attempts:   g.Attempts(),
		DurationMs: durationMs,
		CreatedAt:  g.CreatedAt,
	}
	return s.db.Create(&rec)
}

// leaderboard 返回胜率最高的前 limit 名。
//
// 只统计 played >= minGames 的玩家，避免「只打了一两局全胜」挤掉长期玩家；
// 排序按胜率而非总胜场，避免靠刷局数霸榜。
func (s *store) leaderboard(limit, minGames int) ([]StatRecord, error) {
	var recs []StatRecord
	if err := s.db.Where("played >= ?", max(minGames, 1)).
		Order("CAST(won AS REAL) / played DESC").
		Order("won DESC").
		Order("user_id ASC").
		Limit(limit).
		Find(&recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// probeTrail 把探测序列序列化为 "attr=value" 文本，便于对局记录里回放思路。
func probeTrail(probes []Probe) []string {
	out := make([]string, len(probes))
	for i, p := range probes {
		out[i] = p.Attr.String() + "=" + p.Value
	}
	return out
}

// guessTrail 把猜曲目序列序列化为 "guess=曲名" 文本。
func guessTrail(guesses []TrackGuess) []string {
	out := make([]string, len(guesses))
	for i, gt := range guesses {
		out[i] = "guess=" + gt.Track.Title
	}
	return out
}
