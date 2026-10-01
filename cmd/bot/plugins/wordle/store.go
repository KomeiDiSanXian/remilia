package wordle

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

// StatRecord 每用户的累计统计（GORM 模型）。
type StatRecord struct {
	ID uint `gorm:"primaryKey;autoIncrement"`

	UserID string `gorm:"column:user_id;uniqueIndex:idx_wordle_stat_user;not null;size:128"`

	Played     int `gorm:"not null"`
	Won        int `gorm:"not null"`
	Streak     int `gorm:"not null"`
	BestStreak int `gorm:"not null"`

	// GuessDist 猜中所需次数的分布 JSON，下标 0 表示失败局数。
	GuessDist string `gorm:"type:text"`
	// LastDailyDay 最后一次参与每日题的日期（YYYY-MM-DD）。
	LastDailyDay string `gorm:"size:16"`

	UpdatedAt time.Time
}

// TableName 指定统计表名。
func (StatRecord) TableName() string { return "wordle_stats" }

// DailyLock 记录某个维度当日是否已经开过每日题。
type DailyLock struct {
	Key       string `gorm:"column:lock_key;primaryKey;size:160"`
	Day       string `gorm:"size:16"`
	UpdatedAt time.Time
}

// TableName 指定每日锁表名。
func (DailyLock) TableName() string { return "wordle_daily_locks" }

// GameRecord 已完成对局的历史记录（GORM 模型）。
type GameRecord struct {
	ID uint `gorm:"primaryKey;autoIncrement"`

	Platform string `gorm:"index;size:32"`
	ChatID   string `gorm:"index;size:128"`
	UserID   string `gorm:"index;size:128"`
	Scope    string `gorm:"size:16"`
	Mode     string `gorm:"size:16"`
	// Rules / Modifiers 记录本局启用的玩法，便于回放与统计。
	Rules     string `gorm:"size:64"`
	Modifiers string `gorm:"size:64"`

	Length int
	// Answer 保存全部谜底，多谜底用 "/" 连接。
	Answer     string `gorm:"size:64"`
	Guesses    string `gorm:"type:text"` // 猜测序列 JSON
	Won        bool
	Attempts   int
	DurationMs int64
	// Boards 记录本局棋盘（谜底）数量。
	Boards int
	// ChainIndex 连锁模式下这是第几题（0 起）。
	ChainIndex int

	CreatedAt time.Time `gorm:"index"`
}

// TableName 指定对局表名。
func (GameRecord) TableName() string { return "wordle_games" }

// Stat 内存中的用户统计。
type Stat struct {
	UserID       string
	Played       int
	Won          int
	Streak       int
	BestStreak   int
	GuessDist    [8]int // 下标 0 = 失败，1..7 = 首次猜中的次数
	LastDailyDay string
}

// WinRate 返回胜率（无对局时为 0）。
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
	idx := min(max(attempts, 1), 7)
	s.GuessDist[idx]++
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
		Played:       rec.Played,
		Won:          rec.Won,
		Streak:       rec.Streak,
		BestStreak:   rec.BestStreak,
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
		Played:       st.Played,
		Won:          st.Won,
		Streak:       st.Streak,
		BestStreak:   st.BestStreak,
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
	rec := DailyLock{Key: key, Day: day, UpdatedAt: time.Now()}
	return s.db.Save(&rec)
}

// recordGame 写入一局历史记录。
func (s *store) recordGame(g *Game, userID string, durationMs int64) error {
	guesses, err := json.Marshal(guessWords(g.Guesses))
	if err != nil {
		return err
	}
	rec := GameRecord{
		Platform:   g.Platform,
		ChatID:     g.ChatID,
		UserID:     userID,
		Scope:      g.Scope.String(),
		Mode:       g.Mode.String(),
		Rules:      g.Rules.String(),
		Modifiers:  g.Modifiers.String(),
		Length:     g.Length,
		Answer:     strings.Join(g.Answers, "/"),
		Guesses:    string(guesses),
		Won:        g.Won,
		Attempts:   len(g.Guesses),
		DurationMs: durationMs,
		ChainIndex: g.ChainIndex,
		Boards:     g.BoardCount(),
		CreatedAt:  g.LinkStart(),
	}
	return s.db.Create(&rec)
}

// leaderboard 返回胜场数最高的前 limit 名。
func (s *store) leaderboard(limit int) ([]StatRecord, error) {
	var recs []StatRecord
	if err := s.db.Order("won desc").Limit(limit).Find(&recs); err != nil {
		return nil, err
	}
	return recs, nil
}

func guessWords(guesses []Guess) []string {
	out := make([]string, len(guesses))
	for i, g := range guesses {
		out[i] = g.Word
	}
	return out
}
