package wordle

import (
	"path/filepath"
	"testing"
	"time"

	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
)

func newTestStore(t *testing.T) *store {
	t.Helper()
	db, err := infrastorage.Open(infrastorage.WithDSN(filepath.Join(t.TempDir(), "wordle.db")))
	if err != nil {
		t.Fatalf("打开测试存储失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	s := newStore(db)
	if err := s.migrate(); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return s
}

func TestStore_StatRoundTrip(t *testing.T) {
	s := newTestStore(t)

	st, err := s.loadStat("u1")
	if err != nil {
		t.Fatalf("loadStat 空记录: %v", err)
	}
	if st.Played != 0 || st.Won != 0 {
		t.Fatalf("空记录统计应为零值，实际 %+v", st)
	}

	st.Record(true, 3)
	st.Record(true, 2)
	st.Record(false, 0)
	if err := s.saveStat(st); err != nil {
		t.Fatalf("saveStat: %v", err)
	}

	got, err := s.loadStat("u1")
	if err != nil {
		t.Fatalf("loadStat: %v", err)
	}
	if got.Played != 3 || got.Won != 2 {
		t.Fatalf("Played/Won = %d/%d, 期望 3/2", got.Played, got.Won)
	}
	if got.Streak != 0 || got.BestStreak != 2 {
		t.Fatalf("Streak/Best = %d/%d, 期望 0/2", got.Streak, got.BestStreak)
	}
	if got.GuessDist[3] != 1 || got.GuessDist[2] != 1 || got.GuessDist[0] != 1 {
		t.Fatalf("分布错误: %v", got.GuessDist)
	}
}

func TestStore_SaveStatUpdatesInPlace(t *testing.T) {
	s := newTestStore(t)
	for range 3 {
		st, err := s.loadStat("u2")
		if err != nil {
			t.Fatalf("loadStat: %v", err)
		}
		st.Record(true, 1)
		if err := s.saveStat(st); err != nil {
			t.Fatalf("saveStat: %v", err)
		}
	}
	recs, err := s.leaderboard(10)
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("重复保存应更新同一行，实际 %d 行", len(recs))
	}
	if recs[0].Won != 3 {
		t.Fatalf("Won = %d, 期望 3", recs[0].Won)
	}
}

func TestStore_LeaderboardOrder(t *testing.T) {
	s := newTestStore(t)
	for _, c := range []struct {
		user string
		wins int
	}{{"a", 1}, {"b", 5}, {"c", 3}} {
		st := &Stat{UserID: c.user}
		for i := 0; i < c.wins; i++ {
			st.Record(true, 1)
		}
		if err := s.saveStat(st); err != nil {
			t.Fatalf("saveStat: %v", err)
		}
	}
	recs, err := s.leaderboard(10)
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	if len(recs) != 3 || recs[0].UserID != "b" || recs[1].UserID != "c" {
		t.Fatalf("排行榜排序错误: %+v", recs)
	}
}

func TestStore_DailyLock(t *testing.T) {
	s := newTestStore(t)
	day, err := s.dailyLockDay("chat:qq:g1")
	if err != nil {
		t.Fatalf("dailyLockDay: %v", err)
	}
	if day != "" {
		t.Fatalf("未锁定时应返回空串，实际 %q", day)
	}
	if err := s.setDailyLock("chat:qq:g1", "2026-10-01"); err != nil {
		t.Fatalf("setDailyLock: %v", err)
	}
	day, err = s.dailyLockDay("chat:qq:g1")
	if err != nil || day != "2026-10-01" {
		t.Fatalf("锁定后应为 2026-10-01，实际 %q (%v)", day, err)
	}
	// 覆盖写不应报错
	if err := s.setDailyLock("chat:qq:g1", "2026-10-02"); err != nil {
		t.Fatalf("覆盖每日锁失败: %v", err)
	}
}

func TestStore_RecordGame(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	g := &Game{
		Platform:    "qq",
		ChatID:      "g1",
		OwnerID:     "u1",
		Scope:       ScopeGroup,
		Mode:        ModeDaily,
		Length:      5,
		MaxAttempts: 6,
		Answers:     []string{"crane"},
		Solved:      []bool{false},
		Guesses:     []Guess{{Word: "crane", Marks: Evaluate("crane", "crane")}},
		Won:         true,
		Finished:    true,
		CreatedAt:   now.Add(-time.Minute),
		UpdatedAt:   now,
	}
	if err := s.recordGame(g, "u1", now.Sub(g.CreatedAt).Milliseconds()); err != nil {
		t.Fatalf("recordGame: %v", err)
	}
}

// TestRecordParticipantStats 验证群维度共享棋盘结算时，
// 每位参与者各记一次且同一人重复参与只记一次。
func TestRecordParticipantStats(t *testing.T) {
	s := newTestStore(t)
	p := &Plugin{store: s}
	g := &Game{
		Mode:     ModeRandom,
		Length:   5,
		Answers:  []string{"crane"},
		Solved:   []bool{false},
		Guesses:  []Guess{{Word: "crane"}},
		Won:      true,
		Finished: true,
	}
	g.AddParticipant("u1", "甲")
	g.AddParticipant("u2", "乙")
	g.AddParticipant("u1", "甲")

	p.recordParticipantStats(g, "owner", "2026-10-01")

	for _, uid := range []string{"u1", "u2"} {
		st, err := s.loadStat(uid)
		if err != nil {
			t.Fatalf("loadStat(%s): %v", uid, err)
		}
		if st.Played != 1 || st.Won != 1 || st.GuessDist[1] != 1 {
			t.Fatalf("%s 统计 = %d/%d dist[1]=%d, 期望 1/1/1",
				uid, st.Played, st.Won, st.GuessDist[1])
		}
	}
	if st, _ := s.loadStat("owner"); st.Played != 0 {
		t.Fatal("有参与者时不应再记兜底 owner")
	}
}

// TestRecordParticipantStatsFallback 验证无参与者时以 owner 兜底，
// 并写入每日题标记。
func TestRecordParticipantStatsFallback(t *testing.T) {
	s := newTestStore(t)
	p := &Plugin{store: s}
	g := &Game{Mode: ModeDaily, Length: 5, Won: false, Finished: true}

	p.recordParticipantStats(g, "solo", "2026-10-01")

	st, err := s.loadStat("solo")
	if err != nil {
		t.Fatalf("loadStat: %v", err)
	}
	if st.Played != 1 || st.Won != 0 || st.Streak != 0 {
		t.Fatalf("失败局统计错误: %+v", st)
	}
	if st.LastDailyDay != "2026-10-01" {
		t.Fatalf("LastDailyDay = %q, 期望 2026-10-01", st.LastDailyDay)
	}
}
